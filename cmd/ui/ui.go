package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"path"
	"time"

	"github.com/gorilla/mux"
)

var (
	templates = flag.String("templates", ".", "Template dir")
)

func rootHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles(path.Join(*templates, "main.html")))
	if err := tmpl.Execute(w, nil); err != nil {
		log.Fatalf("Template execute fail: %v", err)
	}
}

func allowHandler(w http.ResponseWriter, r *http.Request) {
	time.Sleep(2 * time.Second)
}

func main() {
	flag.Parse()
	log.Printf("Running...")
	r := mux.NewRouter()
	r.HandleFunc("/", rootHandler).Methods("GET", "HEAD")
	r.HandleFunc("/ajax/allow", allowHandler).Methods("POST")
	http.Handle("/", r)

	http.ListenAndServe(":8080", nil)
}
