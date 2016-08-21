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
	"testing"
)

func TestDecisions(t *testing.T) {
	cfg := Config{
		Rules: map[string]Rule{
			"test": &DomainRule{".habets.se", "allow"},
		},
		Sources: map[string][]string{
			"127.0.0.0/8": []string{"test"},
		},
	}
	*dbFile = "../../testdata/test.sqlite"
	openDB()
	defer db.Close()

	cfg2, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg = *cfg2
	for _, test := range []struct {
		proto, src, method, uri string
		err                     bool
		want                    bool
	}{
		// domain
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, true},
		{"HTTP", "128.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, false},
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.co.uk/", false, false},

		// regex
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/url?foo=bar", false, true},
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/", false, false},

		// https-domain
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:443", false, true},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:8443", false, false},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.co.uk:443", false, false},
	} {
		v, _, err := decide(&cfg, test.proto, test.src, test.method, test.uri)
		if err != nil != test.err {
			t.Fatalf("Want err %v, got %v", test.err, err)
		}
		if v != test.want {
			t.Errorf("Wrong results %q (want %q) for %q", v, test.want, test)
		}
	}
}
