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
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	uuid "github.com/satori/go.uuid"
)

var (
	dbFile   = flag.String("db", "", "sqlite database.")
	name     = flag.String("name", "", "")
	ruleType = flag.String("type", "", "")
	ruleFile = flag.String("file", "", "Rule file.")
	appendID = flag.String("append", "", "Append to ACL")

	db *sql.DB
)

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
	openDB()
	b, err := ioutil.ReadFile(*ruleFile)
	if err != nil {
		log.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	aclID := fmt.Sprint(uuid.NewV4())
	if *appendID != "" {
		aclID = *appendID
	} else {
		if _, err := tx.Exec(`INSERT INTO acls(acl_id, comment) VALUES(?,?)`, aclID, *name); err != nil {
			log.Fatal(err)
		}
	}
	for _, e := range strings.Split(string(b), "\n") {
		if e == "" {
			continue
		}
		id := uuid.NewV4()
		if _, err := tx.Exec(`INSERT INTO rules(rule_id, type, value) VALUES(?, ?, ?)`, id, *ruleType, e); err != nil {
			log.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO aclrules(acl_id, rule_id) VALUES(?, ?)`, aclID, id); err != nil {
			log.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}
