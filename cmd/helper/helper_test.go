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
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"testing"
)

var (
	sqliteBin = "/usr/bin/sqlite3"
)

func TestMain(m *testing.M) {
	var res int
	func() {
		dir, err := ioutil.TempDir("", "squidwarden_test_")
		if err != nil {
			panic(err)
		}

		defer os.RemoveAll(dir) // clean up
		*dbFile = path.Join(dir, "sqidwarden_test.sqlite")

		executeSQL := func(fn string) {
			f, err := os.Open(fn)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			var e bytes.Buffer
			cmd := exec.Command(sqliteBin, *dbFile)
			cmd.Stdin = f
			cmd.Stderr = &e
			if err := cmd.Run(); err != nil {
				log.Fatalf("sqlite setup reading %q: %v, stderr %q", fn, err, e.String())
			}
		}

		executeSQL("../../sqlite.schema")
		executeSQL("../../testdata/test.sql")

		openDB()
		res = m.Run()
	}()
	os.Exit(res)
}

func TestOrder(t *testing.T) {
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ss := []string{
		"127.0.0.1/32",
		"127.0.0.0/8",
		"0.0.0.0/1",
		"129.99.0.1/255.255.0.255",
		"::1234:5678/::ffff:ffff",
	}

	if got, want := len(cfg.Sources), len(ss); got != want {
		t.Fatalf("Got %d sources, want %d", got, want)
	}
	for n, s := range ss {
		if got, want := cfg.Sources[n].source.String(), s; got != want {
			t.Errorf("Got %dth entry %v, want %v", n, got, want)
		}
	}
}

func TestDecisions(t *testing.T) {
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		proto, src, method, uri string
		err                     bool
		want                    bool
	}{
		// domain
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.se:8080/", false, false},
		{"HTTP", "128.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.co.uk/", false, false},

		// CIDR
		{"HTTP", "127.0.0.1", "GET", "http://9.1.2.3/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://9.1.2.3:8080/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://9.1.2.3:8081/blah", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://9.2.2.3/blah", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "9.2.2.3:443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "9.2.2.3:8443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "9.2.2.3:9443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "9.1.2.3:443", false, false},

		// Wildcard port.
		{"HTTP", "127.0.0.1", "GET", "http://9.9.0.1/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://9.9.0.1:80/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://9.9.0.1:8080/blah", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "9.9.0.1", false, false}, // TODO
		{"NONE", "127.0.0.1", "CONNECT", "9.9.0.1:443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "9.9.0.1:8443", false, true},

		// Blocked for local, not for bob.
		// Even though bob is part of local too.
		{"NONE", "127.0.0.1", "CONNECT", "9.10.0.1:443", false, true},
		{"NONE", "127.0.0.2", "CONNECT", "9.10.0.1:443", false, false},

		// domain for literals. Domain with missing port means port 80.
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.4/path/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.4:80/path/blah", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.4:8080/path/blah", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.5/path/blah", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.5:80/path/blah", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://1.2.3.5:8080/path/blah", false, true},

		// regex
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/url?foo=bar", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/", false, false},

		// https-domain
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:8443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.co.uk:443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "www.port.com:443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "www.port.com:8443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "www.github.com:443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "github.com:443", false, true},

		// IPv6 mask
		{"HTTP", "2001:db8::1234:5678", "GET", "http://www.unencrypted.habets.se/", false, true},
		{"HTTP", "2001:db8::1234:5679", "GET", "http://www.unencrypted.habets.se/", false, false},

		// IPv4 mask
		{"HTTP", "129.99.0.1", "GET", "http://www.unencrypted.habets.se/", false, true},
		{"HTTP", "129.99.99.1", "GET", "http://www.unencrypted.habets.se/", false, true},
		{"HTTP", "129.99.0.2", "GET", "http://www.unencrypted.habets.se/", false, false},
		{"HTTP", "129.99.99.2", "GET", "http://www.unencrypted.habets.se/", false, false},
	} {
		v, action, err := decide(cfg, test.proto, test.src, test.method, test.uri)
		if action == actionIgnore {
			v = false
		}
		if err != nil != test.err {
			t.Errorf("Want err %v, got %v for %+v", test.err, err, test)
		} else {
			if v != test.want {
				t.Errorf("Wrong results %t (want %t) for %+v", v, test.want, test)
			}
		}
	}
}
