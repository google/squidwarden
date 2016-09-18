// mkgo takes files and turns them into Go files, to remove external dependencies.
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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

var (
	exts   = flag.String("exts", "", "Comma-separated list of extensions to zip up.")
	dir    = flag.String("dir", "", "Dir full of files.")
	out    = flag.String("out", "", "Output file.")
	prefix = flag.String("prefix", "", "Prefix dir.")
)

func main() {
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatalf("Extra args on cmdline: %q", flag.Args())
	}

	fo, err := os.Create(*out)
	if err != nil {
		log.Fatalf("Opening output %q: %v", *out, err)
	}

	if err := os.Chdir(*dir); err != nil {
		log.Fatalf("chdir(%q): %v", *dir, err)
	}
	l, err := ioutil.ReadDir(".")
	if err != nil {
		log.Fatalf("ReadDir(.) after Chdir(%q): %v", *dir, err)
	}
	if _, err := fo.Write([]byte("package main\nimport \"path\"\nfunc init() {\n")); err != nil {
		log.Fatalf("Writing to %q: %v", *out, err)
	}

	for _, fn := range l {
		fn := fn.Name()
		for _, ext := range strings.Split(*exts, ",") {
			if strings.HasSuffix(fn, "."+ext) {
				b, err := ioutil.ReadFile(fn)
				if err != nil {
					log.Fatalf("Reading %q: %v", fn, err)
				}
				if _, err := fmt.Fprintf(fo, "\tinternalFiles[path.Join(%q,%q)] = []byte(%q)\n", *prefix, fn, string(b)); err != nil {
					log.Fatalf("Writing to %q: %v", *out, err)
				}
			}
		}
	}
	if _, err := fo.Write([]byte("}\n")); err != nil {
		log.Fatalf("Writing to %q: %v", *out, err)
	}
	if err := fo.Close(); err != nil {
		log.Fatalf("Closing %q: %v", *out, err)
	}
}
