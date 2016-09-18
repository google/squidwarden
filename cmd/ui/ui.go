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
	"crypto/rand"
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

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/net/publicsuffix"
)

const (
	version = "0.99"

	actionAllow  = "allow"
	actionBlock  = "block"
	actionIgnore = "ignore"

	typeDomain      = "domain"
	typeHTTPSDomain = "https-domain"
	typeExact       = "exact"
	typeRegex       = "regex"

	saneTime = "2006-01-02 15:04:05 MST"
)

var (
	templates = flag.String("templates", ".", "Template dir")
	staticDir = flag.String("static", ".", "Static dir")
	addr      = flag.String("addr", ":8080", "Address to listen to.")
	squidLog  = flag.String("squidlog", "", "Path to squid log.")
	dbFile    = flag.String("db", "", "sqlite database.")
	httpsOnly = flag.Bool("https_only", true, "Only work with HTTPS.")

	db *sql.DB
)

type aclID string
type acl struct {
	ACLID   aclID
	Comment string
}
type sourceID string
type source struct {
	SourceID sourceID
	Source   string
	Comment  string
}

type ruleID string
type rule struct {
	RuleID  ruleID
	Type    string
	Value   string
	Action  string
	Comment string
}

// given a FQDN, return from the registered domain and on.
// Also support IP literals and with ports.
func host2domain(h string) string {
	if net.ParseIP(h) != nil {
		return h
	}
	if hst, _, err := net.SplitHostPort(h); err == nil && net.ParseIP(hst) != nil {
		return hst
	}
	r, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return h
	}
	return "." + r
}

func rootHandler(r *http.Request) (template.HTML, error) {
	tmpl := template.Must(template.ParseFiles(path.Join(*templates, "main.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func aboutHandler(r *http.Request) (template.HTML, error) {
	tmpl := template.Must(template.ParseFiles(path.Join(*templates, "about.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &struct{ Version string }{Version: version}); err != nil {
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

func ruleNewHandler(r *http.Request) (interface{}, error) {
	data := struct {
		typ    string
		value  string
		action string
	}{
		typ:    r.FormValue("type"),
		value:  r.FormValue("value"),
		action: r.FormValue("action"),
	}
	if data.typ == "" || data.value == "" || data.action == "" {
		return nil, errHTTP{
			internal: nil,
			external: fmt.Sprintf("Missing parameters"),
			code:     http.StatusBadRequest,
		}
	}

	// TODO: look up the ID of the 'new' ACL.
	aclID := aclID("88bf513a-802f-450d-9fc4-b49eeabf1b8f")

	id := uuid.NewV4().String()
	resp := struct {
		Rule string `json:"rule"`
	}{Rule: id}
	return &resp, txWrap(func(tx *sql.Tx) error {
		log.Printf("Adding rule %q", id)
		if _, err := tx.Exec(`INSERT INTO rules(rule_id, action, type, value) VALUES(?,?,?,?)`, id, data.action, data.typ, data.value); err != nil {
			var existing string
			if e := tx.QueryRow(`SELECT rule_id FROM rules WHERE type=? AND value=?`, data.typ, data.value).Scan(&existing); e != nil {
				return errHTTP{
					internal: fmt.Errorf("first %q, then %q", err, e),
					external: "failed to insert rule",
					code:     http.StatusInternalServerError,
				}
			}
			return errHTTP{
				internal: nil,
				external: fmt.Sprintf("refusing to create duplicate of rule %s", existing),
				links: []errHTTPLink{
					{
						Text: "existing rule",
						Link: "/rule/" + existing,
					},
				},
				code: http.StatusConflict,
			}
		}
		if _, err := tx.Exec(`INSERT INTO aclrules(acl_id, rule_id) VALUES(?, ?)`, string(aclID), id); err != nil {
			return err
		}
		return nil
	})
}

func reverse(s []string) []string {
	l := len(s)
	o := make([]string, l, l)
	for i, j := 0, l-1; i < j; i, j = i+1, j-1 {
		o[i], o[j] = s[j], s[i]
	}
	return o
}

type errHTTPLink struct {
	Text string `json:"text"`
	Link string `json:"link"`
}

type errHTTP struct {
	external string
	internal error
	code     int
	links    []errHTTPLink
}

func (e errHTTP) Error() string {
	log.Printf("errHTTP converted to error, losing internal info: %v", e.internal)
	return e.external
}

func errWrapJSON(f func(*http.Request) (interface{}, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		j, err := func() (interface{}, error) {
			// Check that it was requested from JS.
			{
				hn := "X-Requested-With"
				h := r.Header[hn]
				if len(h) != 1 {
					return "", errHTTP{
						internal: fmt.Errorf("possible XSRF attack: Want 1 %s, got %q", hn, h),
						external: fmt.Sprintf("missing or duplicate %s header", hn),
						code:     http.StatusBadRequest,
					}
				}
				if got, want := h[0], "XMLHttpRequest"; got != want {
					return "", errHTTP{
						internal: fmt.Errorf("possible XSRF attack: want %s %q, got %q", hn, want, got),
						external: fmt.Sprintf("bad %s header", hn),
						code:     http.StatusBadRequest,
					}
				}
			}
			// Check that it's not enctype evilness.
			{
				hn := "Content-Type"
				h := r.Header[hn]
				if len(h) != 1 {
					return "", errHTTP{
						internal: fmt.Errorf("possible XSRF attack: Want 1 %s, got %q", hn, h),
						external: fmt.Sprintf("missing or duplicate %s header", hn),
						code:     http.StatusBadRequest,
					}
				}
				if got, want := h[0], "application/x-www-form-urlencoded; "; !strings.HasPrefix(got, want) {
					return "", errHTTP{
						internal: fmt.Errorf("possible XSRF attack: want %s prefixed %q, got %q", hn, want, got),
						external: fmt.Sprintf("bad %s header", hn),
						code:     http.StatusBadRequest,
					}
				}
			}
			return f(r)
		}()
		if err != nil {
			if e, ok := err.(errHTTP); ok {
				log.Printf("HTTP error. External: %q Code: %d. Internal: %v", e.external, e.code, e.internal)
				j = &struct {
					Error string        `json:"error"`
					Links []errHTTPLink `json:"links"`
				}{
					Error: e.external,
					Links: e.links,
				}
				status = e.code
			} else {
				log.Printf("Error in HTTP handler: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
		}
		b, err := json.Marshal(j)
		if err != nil {
			log.Printf("Error marshalling JSON reply: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write(b); err != nil {
			log.Printf("Failed to write JSON reply: %v", err)
		}
	}
}

func errWrap(f func(*http.Request) (template.HTML, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC: %v", err)
				http.Error(w, "Bleh", http.StatusInternalServerError)
			}
		}()
		tmpl := template.Must(template.ParseFiles(path.Join(*templates, "page.html")))
		h, err := f(r)
		if err != nil {
			if e, ok := err.(errHTTP); ok {
				log.Printf("HTTP error. External: %q Code: %d. Internal: %v", e.external, e.code, e.internal)
				http.Error(w, e.external, e.code)
			} else {
				log.Printf("Error in HTTP handler: %v", err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
			}
			return
		}
		if err := tmpl.Execute(w, struct {
			Now     string
			Version string
			CSRF    string
			Content template.HTML
		}{
			Now:     time.Now().UTC().Format(saneTime),
			Version: version,
			CSRF:    csrf.Token(r),
			Content: h,
		}); err != nil {
			log.Printf("Error in main handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}
}

const uuidRE = `[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}`

var reUUID = regexp.MustCompile(`^` + uuidRE + `$`)

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
	resp := struct {
		ACL string `json:"acl"`
	}{ACL: u}
	return &resp, txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO acls(acl_id, comment) VALUES(?,?)`, u, comment); err != nil {
			return err
		}
		return nil
	})
}

func groupNewHandler(r *http.Request) (interface{}, error) {
	comment := r.FormValue("comment")
	if comment == "" {
		return nil, fmt.Errorf("won't create with empty group name")
	}
	u := uuid.NewV4().String()
	resp := struct {
		Group string `json:"group"`
	}{Group: u}
	return &resp, txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO groups(group_id, comment) VALUES(?,?)`, u, comment); err != nil {
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

func membersHandler(r *http.Request) (template.HTML, error) {
	current := groupID(mux.Vars(r)["groupID"])

	type maybeSource struct {
		Active  bool
		Comment string
		Source  source
	}
	data := struct {
		Groups  []group
		Current group
		Sources []maybeSource
	}{}
	{
		var err error
		data.Groups, data.Current, err = getGroups(current)
		if err != nil {
			return "", fmt.Errorf("getGroups: %v", err)
		}
	}
	if len(current) > 0 {
		active, err := getGroupSources(current)
		if err != nil {
			return "", err
		}

		sources, err := getSources()
		if err != nil {
			return "", err
		}
		for _, a := range sources {
			e := maybeSource{Source: a}
			e.Comment, e.Active = active[a.SourceID]
			data.Sources = append(data.Sources, e)
		}
	}

	tmpl := template.Must(template.New("members.html").Funcs(template.FuncMap{
		"groupIDEQ": func(a, b groupID) bool { return a == b },
	}).ParseFiles(path.Join(*templates, "members.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &data); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func getGroups(currentID groupID) ([]group, group, error) {
	var groups []group
	var current group
	rows, err := db.Query(`SELECT group_id, comment FROM groups ORDER BY comment`)
	if err != nil {
		return nil, group{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		var c sql.NullString
		if err := rows.Scan(&s, &c); err != nil {
			return nil, group{}, err
		}
		e := group{
			GroupID: groupID(s),
			Comment: c.String,
		}
		groups = append(groups, e)
		if currentID == e.GroupID {
			current = e
		}
	}
	if err := rows.Err(); err != nil {
		return nil, group{}, err
	}
	return groups, current, nil
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
		var err error
		data.Groups, data.Current, err = getGroups(current)
		if err != nil {
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

func getGroupSources(g groupID) (map[sourceID]string, error) {
	// First see if it exists.
	{
		var t string
		if err := db.QueryRow(`SELECT group_id FROM groups WHERE group_id=?`, string(g)).Scan(&t); err == sql.ErrNoRows {
			return nil, errHTTP{
				internal: err,
				external: fmt.Sprintf("group %q not found", g),
				code:     http.StatusNotFound,
			}
		} else if err != nil {
			return nil, errHTTP{
				internal: err,
				external: fmt.Sprintf("failed looking up group %q", g),
				code:     http.StatusInternalServerError,
			}
		}
	}

	sources := make(map[sourceID]string)

	rows, err := db.Query(`SELECT source_id, comment FROM members WHERE group_id=?`, string(g))
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
		sources[sourceID(s)] = c.String
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
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

func getSources() ([]source, error) {
	var sources []source
	rows, err := db.Query(`SELECT source_id, source, comment FROM sources ORDER BY comment`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		var src string
		var c sql.NullString
		if err := rows.Scan(&s, &src, &c); err != nil {
			return nil, err
		}
		e := source{
			SourceID: sourceID(s),
			Source:   src,
			Comment:  c.String,
		}
		sources = append(sources, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

func formUUIDsStringSlice(vs []string) ([]string, error) {
	var s []string
	for _, u := range vs {
		if !reUUID.MatchString(u) {
			return nil, fmt.Errorf("%q is not valid UUID", u)
		}
		s = append(s, u)
	}
	return s, nil
}

func assertUUID(s string) string {
	if !reUUID.MatchString(s) {
		panic(fmt.Sprintf("%q is not uuid", s))
	}
	return s
}

func assertUUIDOrNull(s string) string {
	if s == "" {
		return ""
	}
	return assertUUID(s)
}

func assertACLID(s string) aclID       { return aclID(assertUUID(s)) }
func assertACLIDOrNull(s string) aclID { return aclID(assertUUIDOrNull(s)) }
func assertGroupID(s string) groupID   { return groupID(assertUUID(s)) }
func assertRuleID(s string) ruleID     { return ruleID(assertUUID(s)) }
func assertSourceID(s string) sourceID { return sourceID(assertUUID(s)) }

func sourceDeleteHandler(r *http.Request) (interface{}, error) {
	sid := assertSourceID(mux.Vars(r)["sourceID"])
	log.Printf("Deleting source %s", sid)
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM sources WHERE source_id=?`, string(sid)); err != nil {
			r := tx.QueryRow(`SELECT COUNT(*) FROM members WHERE source_id=?`, string(sid))
			var n uint64
			if e := r.Scan(&n); e != nil {
				log.Printf("Failed to find member count: %v", e)
				return err
			}
			return errHTTP{
				internal: err,
				external: fmt.Sprintf("source still used by %d groups", n),
				code:     http.StatusBadRequest,
			}
		}
		return nil
	})
}

func groupDeleteHandler(r *http.Request) (interface{}, error) {
	id := assertGroupID(mux.Vars(r)["groupID"])
	log.Printf("Deleting group %s", id)
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM groups WHERE group_id=?`, string(id)); err != nil {
			// Any group members left?
			r := tx.QueryRow(`SELECT COUNT(*) FROM members WHERE group_id=?`, string(id))
			var n uint64
			if e := r.Scan(&n); e != nil {
				log.Printf("Failed to find member count: %v", e)
				return err
			}
			if n > 0 {
				return errHTTP{
					internal: err,
					external: fmt.Sprintf("group still used by %d sources", n),
					code:     http.StatusBadRequest,
				}
			}
			// Any group accesses left?
			r = tx.QueryRow(`SELECT COUNT(*) FROM groupaccess WHERE group_id=?`, string(id))
			if e := r.Scan(&n); e != nil {
				log.Printf("Failed to find groupaccess count: %v", e)
				return err
			}
			if n > 0 {
				return errHTTP{
					internal: err,
					external: fmt.Sprintf("group still granted access to %d acls", n),
					code:     http.StatusBadRequest,
				}
			}
			// No? Then I'm out of ideas.
			return errHTTP{
				internal: err,
				external: "can't delete group",
				code:     http.StatusInternalServerError,
			}
		}
		return nil
	})
}

func aclDeleteHandler(r *http.Request) (interface{}, error) {
	id := assertSourceID(mux.Vars(r)["aclID"])
	log.Printf("Deleting ACL %s", id)
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM acls WHERE acl_id=?`, string(id)); err != nil {
			r := tx.QueryRow(`SELECT COUNT(*) FROM aclrules WHERE acl_id=?`, string(id))
			var n uint64
			if e := r.Scan(&n); e != nil {
				log.Printf("Failed to find rule count: %v", e)
				return err
			}
			return errHTTP{
				internal: err,
				external: fmt.Sprintf("acl still has %d rules", n),
				code:     http.StatusBadRequest,
			}
		}
		return nil
	})
}

func membersNewHandler(r *http.Request) (interface{}, error) {
	gid := assertGroupID(mux.Vars(r)["groupID"])
	r.ParseForm()
	data := struct {
		source        string
		sourceComment string
		comment       string
	}{
		source:        r.FormValue("source"),
		sourceComment: r.FormValue("source-comment"),
		comment:       r.FormValue("comment"),
	}
	u := assertSourceID(uuid.NewV4().String())
	log.Printf("Creating member %s in %s", u, gid)
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO sources(source_id, source, comment) VALUES(?,?,?)`, string(u), data.source, data.sourceComment); err != nil {
			var existing string
			if e := tx.QueryRow(`SELECT source_id FROM sources WHERE source=?`, data.source).Scan(&existing); e != nil {
				return errHTTP{
					internal: fmt.Errorf("first %q, then %q", err, e),
					external: "failed to insert source",
					code:     http.StatusInternalServerError,
				}
			}
			return errHTTP{
				internal: nil,
				external: fmt.Sprintf("refusing to create duplicate of source %s", existing),
				links: []errHTTPLink{
					{
						Text: "existing source",
						Link: "/source/" + existing,
					},
				},
				code: http.StatusConflict,
			}
		}
		if _, err := tx.Exec(`INSERT INTO members(group_id, source_id, comment) VALUES(?,?,?)`, string(gid), string(u), data.comment); err != nil {
			return err
		}
		return nil
	})
}

func membersmembersHandler(r *http.Request) (interface{}, error) {
	r.ParseForm()
	gid := assertGroupID(mux.Vars(r)["groupID"])
	sources, err := formUUIDsStringSlice(r.Form["sources[]"])
	if err != nil {
		return nil, err
	}
	comments := []string(r.Form["comments[]"])

	log.Printf("Updating group %s to %v", gid, sources)
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE from members WHERE group_id=?`, string(gid)); err != nil {
			return err
		}
		for n := range sources {
			if _, err := tx.Exec(`INSERT INTO members(group_id, source_id, comment) VALUES(?,?,?)`, string(gid), sources[n], comments[n]); err != nil {
				return err
			}
		}
		return nil
	})
}

func ruleDeleteHandler(r *http.Request) (interface{}, error) {
	r.ParseForm()
	rules, err := formUUIDsStringSlice(r.Form["rules[]"])
	if err != nil {
		return nil, err
	}
	log.Printf("Deleting %s", strings.Join(rules, ", "))
	return "OK", txWrap(func(tx *sql.Tx) error {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM aclrules WHERE rule_id IN ('%s')`, strings.Join(rules, "','"))); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM rules WHERE rule_id IN ('%s')`, strings.Join(rules, "','"))); err != nil {
			return err
		}
		return nil
	})
}

func ruleEditHandler(r *http.Request) (interface{}, error) {
	ruleID := ruleID(mux.Vars(r)["ruleID"])
	r.ParseForm()

	// Data
	data := struct {
		action  string
		typ     string
		value   string
		comment string
	}{
		action:  r.FormValue("action"),
		typ:     r.FormValue("type"),
		value:   r.FormValue("value"),
		comment: r.FormValue("comment"),
	}
	log.Printf("Updating %q with %+v", ruleID, data)
	return "OK", txWrap(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE rules SET type=?, value=?, action=?, comment=? WHERE rule_id=?`, data.typ, data.value, data.action, data.comment, string(ruleID))
		return err
	})
}

func ruleListHandler(r *http.Request) (template.HTML, error) {
	return "TODO", nil
}

func ruleHandler(r *http.Request) (template.HTML, error) {
	current := assertRuleID(mux.Vars(r)["ruleID"])

	data := struct {
		Current rule
		ACLs    []acl
	}{
		Current: rule{
			RuleID: current,
		},
	}

	// Load rule.
	var c sql.NullString
	if err := db.QueryRow(`SELECT type, value, action, comment FROM rules WHERE rule_id=? `, string(current)).Scan(&data.Current.Type, &data.Current.Value, &data.Current.Action, &c); err == sql.ErrNoRows {
		return "", errHTTP{
			external: "rule not found",
			code:     http.StatusNotFound,
		}
	} else if err != nil {
		return "", err
	}
	data.Current.Comment = c.String

	// Load ACLs.
	rows, err := db.Query(`
SELECT acls.acl_id,acls.comment
FROM aclrules
JOIN acls ON aclrules.acl_id=acls.acl_id
WHERE aclrules.rule_id=?
ORDER BY acls.comment`, string(current))
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

		data.ACLs = append(data.ACLs, acl{
			ACLID:   aclID(s),
			Comment: c.String,
		})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	// Render output
	tmpl := template.Must(template.New("rule.html").ParseFiles(path.Join(*templates, "rule.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &data); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func sourceHandler(r *http.Request) (template.HTML, error) {
	current := assertSourceID(mux.Vars(r)["sourceID"])

	data := struct {
		Current source
		Groups  []group
	}{
		Current: source{
			SourceID: current,
		},
	}

	// Load rule.
	var c sql.NullString
	if err := db.QueryRow(`SELECT source, comment FROM sources WHERE source_id=? `, string(current)).Scan(&data.Current.Source, &c); err == sql.ErrNoRows {
		return "", errHTTP{
			external: "source not found",
			code:     http.StatusNotFound,
		}
	} else if err != nil {
		return "", err
	}
	data.Current.Comment = c.String

	// Load ACLs.
	rows, err := db.Query(`
SELECT groups.group_id, groups.comment
FROM groups
JOIN members ON groups.group_id=members.group_id
WHERE members.source_id=?
ORDER BY groups.comment`, string(current))
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

		data.Groups = append(data.Groups, group{
			GroupID: groupID(s),
			Comment: c.String,
		})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	// Render output
	tmpl := template.Must(template.New("source.html").ParseFiles(path.Join(*templates, "source.html")))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &data); err != nil {
		return "", fmt.Errorf("template execute fail: %v", err)
	}
	return template.HTML(buf.String()), nil
}

func aclHandler(r *http.Request) (template.HTML, error) {
	current := assertACLIDOrNull(mux.Vars(r)["aclID"])

	data := struct {
		ACLs []acl

		Current acl
		Rules   []rule
		Actions []string
		Types   []string
	}{
		Actions: []string{actionAllow, actionIgnore},
		Types:   []string{typeDomain, typeHTTPSDomain, typeRegex, typeExact},
	}
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
	// First check that it exists.
	{
		var t string
		if err := db.QueryRow(`SELECT acl_id FROM acls WHERE acl_id=?`, string(id)).Scan(&t); err == sql.ErrNoRows {
			return nil, errHTTP{
				internal: err,
				external: fmt.Sprintf("ACL %q not found", id),
				code:     http.StatusNotFound,
			}
		} else if err != nil {
			return nil, errHTTP{
				internal: err,
				external: fmt.Sprintf("failed looking up ACL %q", id),
				code:     http.StatusInternalServerError,
			}
		}
	}
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

func getCSRFKey() []byte {
	l := 32
	k := make([]byte, l, l)
	if n, err := rand.Read(k); err != nil {
		if n != l {
			panic(fmt.Sprintf("want %d random bytes, got %d", l, n))
		}
	}
	return k
}

type csrfFail struct{}

func (csrfFail) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("CSRF error with %q: %v", r.FormValue("csrf"), csrf.FailureReason(r))
	http.Error(w, "Forbidden - CSRF token invalid", http.StatusForbidden)
}

func makeRouter() *mux.Router {
	r := mux.NewRouter()

	rget := r.Methods("GET", "HEAD").Subrouter()
	rpost := r.Methods("POST").Subrouter()
	rdelete := r.Methods("DELETE").Subrouter()

	u := uuidRE
	r.HandleFunc("/ajax/tail-log", tailLogHandler).Methods("GET")
	r.HandleFunc("/ajax/tail-log/stream", tailHandler)

	pg := "{groupID:" + u + "}"
	pa := "{aclID:" + u + "}"
	pr := "{ruleID:" + u + "}"
	ps := "{sourceID:" + u + "}"

	for _, e := range []struct {
		path    string
		js      bool
		r       *mux.Router
		handler interface{}
	}{
		{path.Join("/"), false, rget, rootHandler},

		{path.Join("/about"), false, rget, aboutHandler},

		{path.Join("/access") + "/", false, rget, accessHandler},
		{path.Join("/access", pg), false, rget, accessHandler},
		{path.Join("/access", pg), true, rpost, accessUpdateHandler},

		{path.Join("/acl") + "/", false, rget, aclHandler},
		{path.Join("/acl/", pa), false, rget, aclHandler},
		{path.Join("/acl/", pa), true, rdelete, aclDeleteHandler},
		{path.Join("/acl/move"), true, rpost, aclMoveHandler},
		{path.Join("/acl/new"), true, rpost, aclNewHandler},

		{path.Join("/group/", pg), true, rdelete, groupDeleteHandler},
		{path.Join("/group/new"), true, rpost, groupNewHandler},

		{path.Join("/members") + "/", false, rget, membersHandler},
		{path.Join("/members/", pg), false, rget, membersHandler},
		{path.Join("/members/", pg, "members"), true, rpost, membersmembersHandler},
		{path.Join("/members/", pg, "new"), true, rpost, membersNewHandler},

		{path.Join("/rule/") + "/", false, rget, ruleHandler},
		{path.Join("/rule/", pr), false, rget, ruleHandler},
		{path.Join("/rule/", pr), true, rpost, ruleEditHandler},
		{path.Join("/rule/new"), true, rpost, ruleNewHandler},
		{path.Join("/rule/delete"), true, rpost, ruleDeleteHandler},

		{path.Join("/source/", ps), false, rget, sourceHandler},
		{path.Join("/source/", ps), true, rdelete, sourceDeleteHandler},
	} {
		if e.js {
			e.r.HandleFunc(e.path, errWrapJSON(e.handler.(func(*http.Request) (interface{}, error))))
		} else {
			e.r.HandleFunc(e.path, errWrap(e.handler.(func(*http.Request) (template.HTML, error))))
		}
	}
	return r
}
func main() {
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatalf("Extra args on cmdline: %q", flag.Args())
	}
	openDB()

	r := makeRouter()

	fs := http.FileServer(http.Dir(*staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.Handle("/", csrf.Protect(getCSRFKey(),
		csrf.FieldName("csrf"),
		csrf.CookieName("csrf"),
		csrf.Secure(*httpsOnly),
		csrf.Path("/"),
		csrf.ErrorHandler(csrfFail{}))(r))

	log.Printf("Running...")
	log.Fatal(http.ListenAndServe(*addr, nil))
}
