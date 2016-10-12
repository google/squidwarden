package main

import (
	"testing"
)

func TestParseLogEntry(t *testing.T) {
	for _, test := range []struct {
		in   string
		want logEntry
	}{
		{
			"1451606400 10 10.0.0.1 DENIED 100 GET http://blog.habets.se/ - HIER/- foo/bar",
			logEntry{
				Time:   "2016-01-01 00:00:00 UTC",
				Client: "10.0.0.1",
				Method: "GET",
				Domain: ".habets.se",
				Host:   "blog.habets.se",
				Path:   "/",
				URL:    "http://blog.habets.se/",
			},
		},
		{
			"1451606400 10 10.0.0.1 DENIED 100 CONNECT blog.habets.se:443 - HIER/- foo/bar",
			logEntry{
				Time:   "2016-01-01 00:00:00 UTC",
				Client: "10.0.0.1",
				Method: "CONNECT",
				Domain: ".habets.se",
				Host:   "blog.habets.se",
				URL:    "blog.habets.se:443",
			},
		},
		{
			"1451606400 10 10.0.0.1 DENIED 100 CONNECT shell.habets.se:22 - HIER/- foo/bar",
			logEntry{
				Time:   "2016-01-01 00:00:00 UTC",
				Client: "10.0.0.1",
				Method: "CONNECT",
				Domain: ".habets.se:22",
				Host:   "shell.habets.se:22",
				URL:    "shell.habets.se:22",
			},
		},
	} {
		got, err := parseLogEntry(test.in)
		if err != nil {
			t.Errorf("Failed to parse %q: %v", test.in, err)
			continue
		}
		if *got != test.want {
			t.Errorf("%q: got %+v, want %+v", test.in, *got, test.want)
		}
	}
}

func TestHost2Domain(t *testing.T) {
	for _, test := range []struct {
		in, out string
	}{
		{"internal", "internal"},
		{"example.com", ".example.com"},
		{"www.example.com", ".example.com"},
		{"www.foo.bar.example.com", ".example.com"},
		{"example.pp.se", ".example.pp.se"},
		{"www.example.co.uk", ".example.co.uk"},
		{"www.example.com.br", ".example.com.br"},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4:8080", "1.2.3.4"},
	} {
		if got := host2domain(test.in); got != test.out {
			t.Errorf("got %q, want %q", got, test.out)
		}
	}
}
