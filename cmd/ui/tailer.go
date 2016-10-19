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

	"github.com/fsnotify/fsnotify"
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

	//log.Printf("Creating websocket...")
	conn, err := wsupgrade.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade failed: %v", err)
		http.Error(w, "Upgrade failed", http.StatusBadRequest)
		return
	}
	defer func() {
		//log.Printf("Closing websocket")
		conn.Close()
	}()
	changeTick := make(chan struct{}, 1)
	changeTick <- struct{}{}

	// Tick every time the file changes, or 10s if it doesn't.
	{
		w, err := fsnotify.NewWatcher()
		if err != nil {
			log.Fatalf("Couldn't create watcher: %v", err)
		}
		defer w.Close()
		if err := w.Add(*squidLog); err != nil {
			log.Fatalf("Couldn't add watcher on %s: %v", *squidLog, err)
		}
		go func() {
			defer close(changeTick)
			for {
				select {
				case _, ok := <-w.Events:
					//log.Printf("Event %v!", ok)
					if !ok {
						return
					}
				case <-time.After(10 * time.Second):
				}
				select {
				case changeTick <- struct{}{}:
				default:
				}
			}
		}()
	}

	sleep := false
	first := true
	done := websocketDone(conn)
	for {
		if sleep {
			<-changeTick
		}
		sleep = false
		if done() {
			return
		}

		if _, err := f.Seek(pos, 0); err != nil {
			log.Printf("File seek failed: %v", err)
			return
		}
		rd := bufio.NewReader(f)
		line, err := rd.ReadString('\n')
		if err == io.EOF {
			sleep = true
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				log.Printf("Ping failed: %v", err)
				return
			}
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
			sleep = true
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
			//log.Printf("Message write failed: %v", err)
			return
		}
	}
}

func websocketDone(c *websocket.Conn) func() bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		typ, _, err := c.ReadMessage()
		if websocket.IsCloseError(err, websocket.CloseGoingAway) {
			return
		}
		if err != nil {
			log.Printf("Error reading message: %v %v", typ, err)
			return
		}
		if typ == websocket.CloseMessage {
			log.Printf("Socket closed using close message")
			return
		}
	}()
	return func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
}
