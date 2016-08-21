package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	uuid "github.com/satori/go.uuid"
)

const (
	actionAllow  = "allow"
	actionBlock  = "block"
	actionIgnore = "ignore"
)

var (
	templates = flag.String("templates", ".", "Template dir")
	staticDir = flag.String("static", ".", "Static dir")
	addr      = flag.String("addr", ":8080", "Address to listen to.")
	squidLog  = flag.String("squidlog", "", "Path to squid log.")
	dbFile    = flag.String("db", "", "sqlite database.")

	db *sql.DB
)

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

func rootHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles(path.Join(*templates, "main.html")))
	if err := tmpl.Execute(w, nil); err != nil {
		log.Fatalf("Template execute fail: %v", err)
	}
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
	openDB()
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

		entries = append(entries, logEntry{
			Time:   s[1],
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
	log.Printf("Running...")
	r := mux.NewRouter()
	r.HandleFunc("/", rootHandler).Methods("GET", "HEAD")
	r.HandleFunc("/ajax/allow", allowHandler).Methods("POST")
	r.HandleFunc("/ajax/tail-log", tailLogHandler).Methods("GET")

	fs := http.FileServer(http.Dir(*staticDir))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(*addr, nil))
}
