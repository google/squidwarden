package main

import (
	"testing"
)

func TestHost2Domain(t *testing.T) {
	for _, test := range []struct {
		in, out string
	}{
		{"example.com", ".example.com"},
		{"www.example.com", ".example.com"},
		{"www.foo.bar.example.com", ".example.com"},
		{"example.pp.se", ".example.pp.se"},
		{"www.example.co.uk", ".example.co.uk"},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4:8080", "1.2.3.4"},
	} {
		if got := host2domain(test.in); got != test.out {
			t.Errorf("got %q, want %q", got, test.out)
		}
	}
}
