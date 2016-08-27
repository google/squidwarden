/*
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
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	uuid "github.com/satori/go.uuid"
)

const (
	actionAllow  = "allow"
	actionBlock  = "block"
	actionIgnore = "ignore"

	saneTime = "2006-01-02 15:04:05 MST"
)

var (
	templates = flag.String("templates", ".", "Template dir")
	staticDir = flag.String("static", ".", "Static dir")
	addr      = flag.String("addr", ":8080", "Address to listen to.")
	squidLog  = flag.String("squidlog", "", "Path to squid log.")
	dbFile    = flag.String("db", "", "sqlite database.")

	db *sql.DB
)

type aclID string
type acl struct {
	ACLID   aclID
	Comment string
}
type ruleID string
type rule struct {
	RuleID  ruleID
	Type    string
	Value   string
	Action  string
	Comment string
}

func host2domain(h string) string {
	if net.ParseIP(h) != nil {
		return h
	}
	if hst, _, err := net.SplitHostPort(h); err == nil && net.ParseIP(hst) != nil {
		return hst
	}
	tld := 1
	for _, d := range []string{
		".pp.se",
		".co.uk",
		".gov.uk",
	} {
		if strings.HasSuffix(h, d) {
			tld = 2
			break
		}
	}
	s := strings.Split(h, ".")
	if len(s) <= tld {
		return "." + h
	}
	return "." + strings.Join(s[len(s)-tld-1:], ".")
}

func rootHandler(r *http.Request) (template.HTML, error) {
	tmpl := template.Must(template.ParseFiles(path.Join(*templates, "main.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
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

func allowHandler(w http.ResponseWriter, r *http.Request) {
	typ := r.FormValue("type")
	value := r.FormValue("value")
	action := r.FormValue("action")
	if typ == "" || value == "" || action == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}
	// TODO: look up the ID of the 'new' ACL.
	aclID := "88bf513a-802f-450d-9fc4-b49eeabf1b8f"
	if err := txWrap(func(tx *sql.Tx) error {
		id := uuid.NewV4().String()
		log.Printf("Adding rule %q", id)
		if _, err := tx.Exec(`INSERT INTO rules(rule_id, action, type, value) VALUES(?,?,?,?)`, id, action, typ, value); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO aclrules(acl_id, rule_id) VALUES(?, ?)`, aclID, id); err != nil {
			return err
		}
		return nil
	}); err != nil {
		log.Printf("Database trouble: %v", err)
		http.Error(w, "DB problems", http.StatusInternalServerError)
	}
}

func reverse(s []string) []string {
	l := len(s)
	o := make([]string, l, l)
	for i, j := 0, l-1; i < j; i, j = i+1, j-1 {
		o[i], o[j] = s[j], s[i]
	}
	return o
}

func errWrapJSON(f func(*http.Request) (interface{}, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		j, err := f(r)
		if err != nil {
			log.Printf("Error in HTTP handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		b, err := json.Marshal(j)
		if err != nil {
			log.Printf("Error marshalling JSON reply: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(b); err != nil {
			log.Printf("Failed to write JSON reply: %v", err)
		}
	}
}

func errWrap(f func(*http.Request) (template.HTML, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles(path.Join(*templates, "page.html")))
		h, err := f(r)
		if err != nil {
			log.Printf("Error in HTTP handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if err := tmpl.Execute(w, struct {
			Now     string
			Content template.HTML
		}{
			Now:     time.Now().UTC().Format(saneTime),
			Content: h,
		}); err != nil {
			log.Printf("Error in main handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}
}

var reUUID = regexp.MustCompile(`^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$`)

func txWrap(f func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func aclNewHandler(r *http.Request) (interface{}, error) {
	comment := r.FormValue("comment")
	if comment == "" {
		return nil, fmt.Errorf("won't create empty ACL name")
	}
	u := uuid.NewV4().String()
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO acls(acl_id, comment) VALUES(?,?)`, u, comment); err != nil {
			return err
		}
		return nil
	})
}

func aclMoveHandler(r *http.Request) (interface{}, error) {
	r.ParseForm()
	dst := r.FormValue("destination")
	var rules []string
	for _, ruleID := range r.Form["rules[]"] {
		if !reUUID.MatchString(ruleID) {
			return nil, fmt.Errorf("%q is not valid rule ID", ruleID)
		}
		rules = append(rules, ruleID)
	}
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE aclrules SET acl_id=? WHERE rule_id IN ('%s')`, strings.Join(rules, "','")), dst); err != nil {
			return err
		}
		return nil
	})
}

func accessUpdateHandler(r *http.Request) (interface{}, error) {
	groupID := groupID(mux.Vars(r)["groupID"])
	r.ParseForm()

	var acls []string
	for _, aclID := range r.Form["acls[]"] {
		if !reUUID.MatchString(aclID) {
			return nil, fmt.Errorf("%q is not valid acl ID", aclID)
		}
		acls = append(acls, aclID)
	}

	comments := r.Form["comments[]"]
	if len(comments) != len(acls) {
		return nil, fmt.Errorf("acl list and comment list length unequal. acl=%d comment=%d", len(acls), len(comments))
	}

	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM groupaccess WHERE group_id=?`, string(groupID)); err != nil {
			return err
		}
		for n := range acls {
			if _, err := tx.Exec(`INSERT INTO groupaccess(group_id, acl_id, comment) VALUES(?,?,?)`, string(groupID), acls[n], comments[n]); err != nil {
				return err
			}
		}
		return nil
	})
}

type groupID string
type group struct {
	GroupID groupID
	Comment string
}

func accessHandler(r *http.Request) (template.HTML, error) {
	current := groupID(mux.Vars(r)["groupID"])

	type maybeACL struct {
		Active  bool
		Comment string
		ACL     acl
	}
	data := struct {
		Groups  []group
		Current group
		ACLs    []maybeACL
	}{}
	{
		rows, err := db.Query(`SELECT group_id, comment FROM groups ORDER BY comment`)
		if err != nil {
			return "", err
		}
		defer rows.Close()

		for rows.Next() {
			var s string
			var c sql.NullString
			if err := rows.Scan(&s, &c); err != nil {
				return "", err
			}
			e := group{
				GroupID: groupID(s),
				Comment: c.String,
			}
			data.Groups = append(data.Groups, e)
			if current == e.GroupID {
				data.Current = e
			}
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	}
	if len(current) > 0 {
		active, err := getGroupACLs(current)
		if err != nil {
			return "", err
		}

		acls, err := getACLs()
		if err != nil {
			return "", err
		}
		for _, a := range acls {
			e := maybeACL{ACL: a}
			e.Comment, e.Active = active[a.ACLID]
			data.ACLs = append(data.ACLs, e)
		}
	}

	tmpl := template.Must(template.New("access.html").Funcs(template.FuncMap{
		"groupIDEQ": func(a, b groupID) bool { return a == b },
	}).ParseFiles(path.Join(*templates, "access.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &data); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func getGroupACLs(g groupID) (map[aclID]string, error) {
	acls := make(map[aclID]string)

	rows, err := db.Query(`SELECT acl_id, comment FROM groupaccess WHERE group_id=?`, string(g))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		var c sql.NullString
		if err := rows.Scan(&s, &c); err != nil {
			return nil, err
		}
		acls[aclID(s)] = c.String
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return acls, nil
}

func getACLs() ([]acl, error) {
	var acls []acl
	rows, err := db.Query(`SELECT acl_id, comment FROM acls ORDER BY comment`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		var c sql.NullString
		if err := rows.Scan(&s, &c); err != nil {
			return nil, err
		}
		e := acl{
			ACLID:   aclID(s),
			Comment: c.String,
		}
		acls = append(acls, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return acls, nil
}

func aclHandler(r *http.Request) (template.HTML, error) {
	current := aclID(mux.Vars(r)["aclID"])

	data := struct {
		ACLs []acl

		Current acl
		Rules   []rule
	}{}
	{
		rows, err := db.Query(`SELECT acl_id, comment FROM acls ORDER BY comment`)
		if err != nil {
			return "", err
		}
		defer rows.Close()

		for rows.Next() {
			var s string
			var c sql.NullString
			if err := rows.Scan(&s, &c); err != nil {
				return "", err
			}
			e := acl{
				ACLID:   aclID(s),
				Comment: c.String,
			}
			if current == e.ACLID {
				data.Current = e
			}
			data.ACLs = append(data.ACLs, e)
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	}

	if len(current) > 0 {
		r, err := loadACL(current)
		if err != nil {
			return "", err
		}
		data.Rules = r
	}

	tmpl := template.Must(template.New("acl.html").Funcs(template.FuncMap{
		"aclIDEQ": func(a, b aclID) bool { return a == b },
	}).ParseFiles(path.Join(*templates, "acl.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &data); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func loadACL(id aclID) ([]rule, error) {
	rows, err := db.Query(`
SELECT rules.rule_id, rules.type, rules.value, rules.action, rules.comment
FROM aclrules
JOIN rules ON aclrules.rule_id=rules.rule_id
WHERE aclrules.acl_id=?
ORDER BY rules.comment, rules.type, rules.value`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []rule
	for rows.Next() {
		var e rule
		var s string
		var c sql.NullString
		if err := rows.Scan(&s, &e.Type, &e.Value, &e.Action, &c); err != nil {
			return nil, err
		}
		e.RuleID = ruleID(s)
		e.Comment = c.String
		rules = append(rules, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

type logEntry struct {
	Time   string
	Client string
	Method string
	Domain string
	Host   string
	Path   string
	URL    string
}

var errSkip = errors.New("skip this one, don't log")

func parseLogEntry(l string) (*logEntry, error) {
	//                        time        ms    client     DENIED    size   method  URL           HIER    type
	re := regexp.MustCompile(`([0-9.]+)\s+\d+\s+([^\s]+)\s+([^\s]+)\s+\d+\s+(\w+)\s+([^\s]+)\s+-\s[^\s]+\s([^\s]+)`)
	if len(l) == 0 {
		return nil, errSkip
	}
	s := re.FindStringSubmatch(l)
	if len(s) == 0 {
		return nil, fmt.Errorf("bad log line: %q", l)
	}
	var host, p string
	u := s[5]
	if ur, err := url.Parse(u); strings.Contains(u, "/") && err == nil && ur.Scheme != "" {
		host = ur.Host
		p = ur.Path
	} else {
		host, _, err = net.SplitHostPort(u)
		if err != nil {
			host = u
		}
	}

	ts, err := strconv.ParseFloat(s[1], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse epoch time %q: %v", s[1], err)
	}
	return &logEntry{
		Time:   time.Unix(int64(ts), int64(1e9*(ts-math.Trunc(ts)))).UTC().Format(saneTime),
		Client: s[2],
		Method: s[4],
		Domain: host2domain(host),
		Host:   host,
		Path:   p,
		URL:    u,
	}, nil
}

func tailLogHandler(w http.ResponseWriter, r *http.Request) {
	b, err := ioutil.ReadFile(*squidLog)
	if err != nil {
		log.Printf("Failed to read squid log: %v", err)
		return
	}
	lines := reverse(strings.Split(string(b), "\n"))
	const n = 30
	if len(lines) > n {
		lines = lines[:n]
	}
	entries := []*logEntry{}
	for _, l := range lines {
		entry, err := parseLogEntry(l)
		switch err {
		case nil:
			entries = append(entries, entry)
		case errSkip:
		default:
			log.Printf("Parsing log entry: %v", err)
		}
	}
	b, err = json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(b); err != nil {
		log.Printf("Failed writing tail stuff: %v", err)
	}
}

func main() {
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatalf("Extra args on cmdline: %q", flag.Args())
	}
	openDB()
	log.Printf("Running...")
	r := mux.NewRouter()
	r.HandleFunc("/", errWrap(rootHandler)).Methods("GET", "HEAD")
	r.HandleFunc("/acl/", errWrap(aclHandler)).Methods("GET", "HEAD")
	r.HandleFunc("/acl/{aclID}", errWrap(aclHandler)).Methods("GET", "HEAD")
	r.HandleFunc("/acl/move", errWrapJSON(aclMoveHandler)).Methods("POST")
	r.HandleFunc("/acl/new", errWrapJSON(aclNewHandler)).Methods("POST")
	r.HandleFunc("/access/", errWrap(accessHandler)).Methods("GET", "HEAD")
	r.HandleFunc("/access/{groupID}", errWrap(accessHandler)).Methods("GET", "HEAD")
	r.HandleFunc("/access/{groupID}", errWrapJSON(accessUpdateHandler)).Methods("POST")
	r.HandleFunc("/ajax/allow", allowHandler).Methods("POST")
	r.HandleFunc("/ajax/tail-log", tailLogHandler).Methods("GET")
	r.HandleFunc("/ajax/tail-log/stream", tailHandler)

	fs := http.FileServer(http.Dir(*staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(*addr, nil))
}
