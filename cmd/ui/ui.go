package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

	saneTime = "2016-01-02 15:04:05 MST"
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
		return h
	}
	return strings.Join(s[len(s)-tld-1:], ".")
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
	if typ == "" || value == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}
	// TODO: look up 'misc'.
	aclID := "db4c935f-fc5b-4868-ab32-c80459159c3e"
	if err := func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		id := uuid.NewV4()
		log.Printf("Adding rule %q", id)
		if _, err := tx.Exec(`INSERT INTO rules(rule_id, action, type, value) VALUES(?,?,?,?)`, id, actionAllow, typ, value); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO aclrules(acl_id, rule_id) VALUES(?, ?)`, aclID, id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	}(); err != nil {
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

func errWrap(f func(*http.Request) (template.HTML, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles(path.Join(*templates, "page.html")))
		h, err := f(r)
		if err != nil {
			log.Printf("Error in HTTP handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if err := tmpl.Execute(w, struct{ Content template.HTML }{Content: h}); err != nil {
			log.Printf("Error in main handler: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}
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
ORDER BY rules.comment`, string(id))
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
	type logEntry struct {
		Time   string
		Client string
		Method string
		Domain string
		Host   string
		Path   string
		URL    string
	}
	entries := []logEntry{}
	//                        time        ms    client     DENIED    size   method  URL           HIER    type
	re := regexp.MustCompile(`([0-9.]+)\s+\d+\s+([^\s]+)\s+([^\s]+)\s+\d+\s+(\w+)\s+([^\s]+)\s+-\s[^\s]+\s([^\s]+)`)
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		s := re.FindStringSubmatch(l)
		if len(s) == 0 {
			log.Printf("Bad log line: %q", l)
			continue
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
			log.Printf("Failed to parse epoch time %q: %v", s[1], err)
		}

		entries = append(entries, logEntry{
			Time:   time.Unix(int64(ts), int64(1e9*(ts-math.Trunc(ts)))).UTC().Format(saneTime),
			Client: s[2],
			Method: s[4],
			Domain: "." + host2domain(host),
			Host:   host,
			Path:   p,
			URL:    u,
		})
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
	r.HandleFunc("/ajax/allow", allowHandler).Methods("POST")
	r.HandleFunc("/ajax/tail-log", tailLogHandler).Methods("GET")

	fs := http.FileServer(http.Dir(*staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(*addr, nil))
}
