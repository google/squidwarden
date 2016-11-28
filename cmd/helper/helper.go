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
	"sort"
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

type source interface {
	String() string
	Contains(net.IP) bool
	PrefixLen() int
}

type sourceMask struct {
	host net.IP
	mask net.IP
}

func (s *sourceMask) String() string {
	return s.host.String() + "/" + s.mask.String()
}

func (s *sourceMask) Contains(a net.IP) bool {
	for n := range s.host {
		if s.host[n] != a[n]&s.mask[n] {
			return false
		}
	}
	return true
}

func (s *sourceMask) PrefixLen() int {
	// This is used for sorting only.
	// TODO: what should be sorted by?
	return 0
}

type sourceNet net.IPNet

func (s *sourceNet) Contains(a net.IP) bool {
	return (*net.IPNet)(s).Contains(a)
}

func (s *sourceNet) String() string {
	return (*net.IPNet)(s).String()
}

func (s *sourceNet) PrefixLen() int {
	r, _ := s.Mask.Size()
	return r
}

type sourceRule struct {
	source source
	rules  []string
}

type Config struct {
	// Map from source to rules.
	Sources []sourceRule
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

func splitHostPortDefault(s, def string) (string, string) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		host = s
		port = def
	}
	return host, port
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

	// If query doesn't have port, assume port 80.
	ruleHost, rulePort := splitHostPortDefault(d.value, "80")
	hostOnly, portOnly := splitHostPortDefault(p.Host, "80")

	if portOnly != rulePort && rulePort != "*" {
		return false, nil
	}

	// Exact match.
	if hostOnly == ruleHost {
		return true, nil
	}

	// If rule is CIDR, allow anything in it.
	if _, cidr, err := net.ParseCIDR(ruleHost); err == nil {
		if ip := net.ParseIP(hostOnly); ip != nil {
			if cidr.Contains(ip) {
				return true, nil
			}
		}
	}

	// Domain suffix.
	if strings.HasPrefix(d.value, ".") {
		// No extra level.
		if "."+hostOnly == ruleHost {
			return true, nil
		}
		if strings.HasSuffix(hostOnly, ruleHost) {
			return true, nil
		}
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

type HTTPSRegexRule struct {
	re *regexp.Regexp
}

func (d *HTTPSRegexRule) Check(proto, src, method, uri string) (bool, error) {
	if proto != "NONE" {
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
	dhost, dport := splitHostPortDefault(d.value, "443")
	if dhost == "" {
		return false, nil
	}
	host, port, err := net.SplitHostPort(uri)
	if err != nil {
		return false, fmt.Errorf("failed to parse HTTPS host:port %q: %v", uri, err)
	}
	if port != dport && dport != "*" {
		return false, nil
	}
	// Exact hostname.
	if host == dhost {
		return true, nil
	}

	// If rule is CIDR, allow those.
	if _, cidr, err := net.ParseCIDR(dhost); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			if cidr.Contains(ip) {
				return true, nil
			}
		}
	}

	// Domain suffix.
	if strings.HasPrefix(d.value, ".") {
		// No extra level.
		if "."+host == dhost {
			return true, nil
		}
		if strings.HasSuffix(host, dhost) {
			return true, nil
		}
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
	for _, rs := range cfg.Sources {
		if !rs.source.Contains(source) {
			continue
		}
		for _, ruleName := range rs.rules {
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

func parseMask(s string) (source, error) {
	re := regexp.MustCompile(`^([0-9a-fA-F:.]+)/([0-9a-fA-F:.]+)$`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("not a host match")
	}
	a := net.ParseIP(m[1])
	if a == nil {
		return nil, fmt.Errorf("not a valid address: %q", m[1])
	}
	b := net.ParseIP(m[2])
	if b == nil {
		return nil, fmt.Errorf("not a valid address: %q", m[2])
	}
	return &sourceMask{host: a, mask: b}, nil
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		Rules: make(map[string]RuleAction),
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
JOIN rules ON aclrules.rule_id=rules.rule_id
ORDER BY sources.source`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var prevSource source
		var rs []string
		for rows.Next() {
			var src, rule string
			if err := rows.Scan(&src, &rule); err != nil {
				return err
			}
			var s source
			if _, t, err := net.ParseCIDR(src); err != nil {
				if t, err := parseMask(src); err != nil {
					log.Printf("%q is not valid CIDR: %v", src, err)
					continue
				} else {
					s = t
				}
			} else {
				t := sourceNet(*t)
				s = &t
			}
			if prevSource != nil && (prevSource.String() != s.String()) {
				cfg.Sources = append(cfg.Sources, sourceRule{source: prevSource, rules: rs})
				rs = nil
			}
			prevSource = s
			rs = append(rs, rule)
		}
		if prevSource != nil {
			cfg.Sources = append(cfg.Sources, sourceRule{source: prevSource, rules: rs})
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
				x, err := regexp.Compile("^" + val + "$")
				if err != nil {
					return fmt.Errorf("compiling regex %q: %v", val, err)
				}
				r.rule = &RegexRule{re: x}
			case "https-regex":
				x, err := regexp.Compile("^" + val + "$")
				if err != nil {
					return fmt.Errorf("compiling regex %q: %v", val, err)
				}
				r.rule = &HTTPSRegexRule{re: x}
			default:
				return fmt.Errorf("unknown rule type %q", typ)
			}
			cfg.Rules[rule] = r
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(byPrefixLen(cfg.Sources)))
	return cfg, nil
}

type byPrefixLen []sourceRule

func (a byPrefixLen) Len() int      { return len(a) }
func (a byPrefixLen) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byPrefixLen) Less(i, j int) bool {
	return a[i].source.PrefixLen() < a[j].source.PrefixLen()
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
