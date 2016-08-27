/**
external ACL helper for squid.

Configure with:
  external_acl_type ext ttl=10 concurrency=2 %PROTO %SRC %METHOD %URI /usr/local/bin/proxyacl -db=/var/spool/squid3/proxyacl.sqlite -log=/var/log/squid3/proxyacl.log
  acl ext_acl external ext
  http_access allow ext_acl

Copyright 2016 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	dbFile   = flag.String("db", "", "sqlite database.")
	logFile  = flag.String("log", "", "Logfile. Default to stderr.")
	verbose  = flag.Int("v", 1, "Verbosity level.")
	blockLog = flag.String("block_log", "", "Block log.")

	db *sql.DB
)

type action string

const (
	actionNone   action = "none"
	actionBlock  action = "block"
	actionIgnore action = "ignore"
	actionAllow  action = "allow"

	actionDefault = actionBlock

	aclMatch   = "OK"
	aclNoMatch = "ERR"
)

type Config struct {
	// Map from source to rules.
	Sources map[string][]string
	Rules   map[string]RuleAction
}

type Rule interface {
	Check(proto, src, method, uri string) (bool, error)
}

type RuleAction struct {
	rule   Rule
	action action
}

type DomainRule struct {
	value string
}

func (d *DomainRule) Check(proto, src, method, uri string) (bool, error) {
	if proto != "HTTP" {
		return false, nil
	}
	if d.value == "" {
		return false, nil
	}
	p, err := url.Parse(uri)
	if err != nil {
		return false, err
	}

	// Exact hostname.
	if p.Host == d.value {
		return true, nil
	}

	// Domain suffix.
	if d.value[0] == '.' && ("."+p.Host == d.value || strings.HasSuffix(p.Host, d.value)) {
		return true, nil
	}
	return false, nil
}

type RegexRule struct {
	re *regexp.Regexp
}

func (d *RegexRule) Check(proto, src, method, uri string) (bool, error) {
	if proto != "HTTP" {
		return false, nil
	}
	if d.re.MatchString(uri) {
		return true, nil
	}
	return false, nil
}

type ExactRule struct {
	value string
}

func (d *ExactRule) Check(proto, src, method, uri string) (bool, error) {
	if proto != "HTTP" {
		return false, nil
	}
	return d.value == uri, nil
}

type HTTPSDomainRule struct {
	value string
}

func (d *HTTPSDomainRule) Check(proto, src, method, uri string) (bool, error) {
	if proto != "NONE" {
		return false, nil
	}
	if method != "CONNECT" {
		return false, nil
	}
	dhost, dport, err := net.SplitHostPort(d.value)
	if err != nil {
		dhost, dport = d.value, "443"
	}
	if dhost == "" {
		return false, nil
	}
	host, port, err := net.SplitHostPort(uri)
	if err != nil {
		return false, fmt.Errorf("failed to parse HTTPS host:port %q: %v", uri, err)
	}
	if port != dport {
		return false, nil
	}
	// Exact hostname.
	if host == dhost {
		return true, nil
	}

	// Domain suffix.
	if dhost[0] == '.' && ("."+host == dhost || strings.HasSuffix(host, dhost)) {
		return true, nil
	}
	return false, nil
}

// decide returns 'match found', 'action to take', error
func decide(cfg *Config, proto, src, method, uri string) (bool, action, error) {
	// Special case this because net/url can't parse these.
	if strings.HasPrefix(uri, "cache_object://") {
		return true, actionIgnore, nil
	}

	source := net.ParseIP(src)
	if source == nil {
		return false, actionNone, fmt.Errorf("source is not a valid address: %q", src)
	}
	for a, rules := range cfg.Sources {
		_, net, err := net.ParseCIDR(a)
		if err != nil {
			log.Printf("Not a valid source: %q: %v", a, err)
			continue
		}
		if !net.Contains(source) {
			continue
		}
		for _, ruleName := range rules {
			rule := cfg.Rules[ruleName]
			t, err := rule.rule.Check(proto, src, method, uri)
			if err != nil {
				log.Printf("Failed to evaluate rule %q: %v", ruleName, err)
			} else if t {
				return true, rule.action, nil
			}
		}
	}
	return false, actionDefault, nil
}

func mainLoop() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	// TODO: multithread this.
	scanner := bufio.NewScanner(os.Stdin)
	lastLoad := time.Now()
	for scanner.Scan() {
		if time.Since(lastLoad) > time.Second {
			cfg2, err := loadConfig()
			if err != nil {
				log.Printf("Failed to reload database")
			} else {
				cfg = cfg2
			}
			lastLoad = time.Now()
		}

		s := strings.Split(scanner.Text(), " ")
		if *verbose > 1 {
			log.Printf("Got %q", s)
		}
		token := s[0]
		proto := s[1]
		src := s[2]
		method := s[3]
		uri := s[4]
		urip, err := url.QueryUnescape(uri)
		reply := aclNoMatch
		if err != nil {
			log.Printf("URI escape error on %q: %v", s, err)
		} else {
			_, act, err := decide(cfg, proto, src, method, urip)
			if err != nil {
				log.Printf("Decision error on %q: %v", s, err)
			}
			switch act {
			case actionBlock, actionNone:
				if *verbose > 0 && reply != aclMatch {
					log.Printf("No match(%s): %q", act, s)
				}
				if err := logBlock(proto, src, method, urip); err != nil {
					log.Printf("Logging block: %v", err)
				}
			case actionIgnore:
			case actionAllow:
				reply = aclMatch
			}
		}
		if *verbose > 1 {
			log.Printf("Replied: %s %s", token, reply)
		}
		fmt.Printf("%s %s\n", token, reply)
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func logBlock(proto, src, method, urip string) error {
	f, err := os.OpenFile(*blockLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	fmt.Fprintf(f, "%f 0 %s %s %d %s %s - HIER/- foo/bar\n", float64(time.Now().UnixNano())/1e9, src, "DENIED", 0, method, urip)
	if err := f.Sync(); err != nil {
		log.Printf("Failed to sync blockfile: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	return nil
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		Sources: make(map[string][]string),
		Rules:   make(map[string]RuleAction),
	}
	if err := func() error {
		rows, err := db.Query(`
SELECT sources.source, rules.rule_id
FROM sources
JOIN members ON sources.source_id=members.source_id
JOIN groups ON members.group_id=groups.group_id
JOIN groupaccess ON groups.group_id=groupaccess.group_id
JOIN acls ON groupaccess.acl_id=acls.acl_id
JOIN aclrules ON acls.acl_id=aclrules.acl_id
JOIN rules ON aclrules.rule_id=rules.rule_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var source, rule string
			if err := rows.Scan(&source, &rule); err != nil {
				return err
			}
			cfg.Sources[source] = append(cfg.Sources[source], rule)
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}

	if err := func() error {
		rows, err := db.Query(`
SELECT rule_id, type, value, action
FROM rules
`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rule, typ, val, act string
			if err := rows.Scan(&rule, &typ, &val, &act); err != nil {
				return err
			}
			r := RuleAction{action: action(act)}
			switch typ {
			case "https-domain":
				r.rule = &HTTPSDomainRule{value: val}
			case "domain":
				r.rule = &DomainRule{value: val}
			case "exact":
				r.rule = &ExactRule{value: val}
			case "regex":
				x, err := regexp.Compile(val)
				if err != nil {
					return fmt.Errorf("compiling regex %q: %v", val, err)
				}
				r.rule = &RegexRule{re: x}
			default:
				return fmt.Errorf("unknown rule type %q", typ)
			}
			cfg.Rules[rule] = r
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func openDB() {
	var err error
	db, err = sql.Open("sqlite3", *dbFile)
	if err != nil {
		log.Fatalf("Failed to open database %q: %v", *dbFile, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Fatalf("Failed to turn on foreign keys")
	}
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.LUTC)
	if flag.NArg() > 0 {
		log.Fatalf("Extra args on cmdline: %q", flag.Args())
	}
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			log.Fatalf("Opening log file %q: %v", *logFile, err)
		}
		defer f.Close()
		log.SetOutput(f)
	}
	openDB()
	defer db.Close()
	log.Printf("Running...")
	mainLoop()
}
