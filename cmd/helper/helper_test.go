package main

import (
	"testing"
)

func TestDecisions(t *testing.T) {
	cfg := Config{
		Rules: map[string]Rule{
			"test": &DomainRule{".habets.se"},
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
		want                    string
	}{
		// domain
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, "OK"},
		{"HTTP", "128.0.0.1", "GET", "http://www.unencrypted.habets.se/", false, "ERR"},
		{"HTTP", "127.0.0.1", "GET", "http://www.unencrypted.habets.co.uk/", false, "ERR"},

		// regex
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/url?foo=bar", false, "OK"},
		{"HTTP", "127.0.0.1", "GET", "http://www.google.co.uk/", false, "ERR"},

		// https-domain
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:443", false, "OK"},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.se:8443", false, "ERR"},
		{"NONE", "127.0.0.1", "CONNECT", "www.habets.co.uk:443", false, "ERR"},
	} {
		v, err := decide(&cfg, test.proto, test.src, test.method, test.uri)
		if err != nil != test.err {
			t.Fatalf("Want err %v, got %v", test.err, err)
		}
		if v != test.want {
			t.Errorf("Wrong results %q (want %q) for %q", v, test.want, test)
		}
	}
}
