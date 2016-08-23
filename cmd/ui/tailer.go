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
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	wsupgrade = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

const (
	maxLineLength = 1000
)

func tailHandler(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(*squidLog)
	if err != nil {
		log.Printf("File open failed: %v", err)
		http.Error(w, "File open failed", http.StatusInternalServerError)
		return
	}
	pos, err := f.Seek(-maxLineLength, 2)
	if err != nil {
		log.Printf("File seek failed: %v", err)
		http.Error(w, "File seek failed", http.StatusInternalServerError)
		return
	}
	conn, err := wsupgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade failed: %v", err)
		http.Error(w, "Upgrade failed", http.StatusBadRequest)
		return
	}
	defer conn.Close()
	sleep := false
	first := true
	for {
		if sleep {
			time.Sleep(time.Second)
		}
		sleep = true

		if pos, err := f.Seek(pos, 0); err != nil {
			log.Printf("File seek failed: %v", err)
			return
		} else {
			if false {
				log.Printf("Reading at pos %d", pos)
			}
		}
		rd := bufio.NewReader(f)
		line, err := rd.ReadString('\n')
		if err == io.EOF {
			//log.Printf("Read %d EOF", len(line))
			continue
		}
		if err != nil {
			log.Printf("File read error: %v", err)
			return
		}
		if !strings.HasSuffix(line, "\n") {
			// Not a complete line yet.
			if _, err := f.Seek(pos, 0); err != nil {
				log.Printf("File seek failed: %v", err)
				return
			}
			continue
		}
		pos += int64(len(line))

		e, err := parseLogEntry(line)
		if err == errSkip {
		} else if err != nil {
			if !first {
				log.Printf("Error parsing log line: %v", err)
			}
			first = false
			continue
		}
		data, err := json.Marshal(e)
		if err != nil {
			log.Printf("Failed to mashal tail: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Message write failed: %v", err)
			return
		}
		sleep = false
	}
}
