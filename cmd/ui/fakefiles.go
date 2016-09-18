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
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"time"
)

var (
	diskFiles = flag.Bool("disk_files", true, "Try to read files off of disk.")
	memFiles  = flag.Bool("mem_files", true, "Try to read files in memory.")

	internalFiles = make(map[string][]byte)
)

//go:generate go run ../mkgo/mkgo.go -exts=html -dir=templates -prefix=templates -out=templates.go
//go:generate go run ../mkgo/mkgo.go -exts=css,js,gif -dir=static -prefix=static -out=static.go

func readFile(fn string) ([]byte, error) {
	if *memFiles {
		b, found := internalFiles[fn]
		if found {
			return b, nil
		}
	}
	if *diskFiles {
		return ioutil.ReadFile(fn)
	}
	return nil, &os.PathError{
		Op:   "open",
		Path: fn,
		Err:  os.ErrNotExist,
	}
}

type myDir struct {
	r string
}

func (d *myDir) Open(name string) (http.File, error) {
	b, err := readFile(path.Join(d.r, name))
	if err != nil {
		return nil, err
	}
	return &myFile{name, b, 0}, nil
}

type myFile struct {
	name string
	buf  []byte
	pos  int64
}

func (f *myFile) Close() error {
	return nil
}

func (f *myFile) Read(data []byte) (int, error) {
	end := f.pos + int64(len(data))
	if end > int64(len(f.buf)) {
		end = int64(len(f.buf))
	}
	r := f.buf[f.pos:end]
	copy(data, r)
	f.pos += int64(len(r))
	return len(r), nil
}

func (f *myFile) Seek(offset int64, whence int) (int64, error) {
	if whence != 0 {
		panic("seek not fully supported")
	}
	f.pos = offset
	return f.pos, nil
}

func (f *myFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}

type stat struct {
	name string
	size int64
}

func (s *stat) Name() string       { return s.name }
func (s *stat) Size() int64        { return s.size }
func (s *stat) Mode() os.FileMode  { return 0644 }
func (s *stat) ModTime() time.Time { return time.Now() }
func (s *stat) IsDir() bool        { return false }
func (s *stat) Sys() interface{}   { panic("stat.Sys") }

func (f *myFile) Stat() (os.FileInfo, error) {
	return &stat{
		name: f.name,
		size: int64(len(f.buf)),
	}, nil
}
