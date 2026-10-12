// test-backend is an isolated test fixture, never included in the release image.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type behavior struct {
	Status   int   `json:"status"`
	Statuses []int `json:"statuses"`
	DelayMS  int   `json:"delay_ms"`
}
type record struct {
	Backend  string      `json:"backend"`
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	RawQuery string      `json:"raw_query"`
	Headers  http.Header `json:"headers"`
	Body     []byte      `json:"body"`
}
type fixture struct {
	mu       sync.Mutex
	behavior behavior
	count    int
	active   int
	records  []record
	name     string
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(200)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		http.Error(w, "body too large", 413)
		return
	}
	entry := record{f.name, r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body}
	f.mu.Lock()
	index := f.count
	f.count++
	f.active++
	b := f.behavior
	if len(f.records) < 1000 {
		f.records = append(f.records, entry)
	}
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	if b.DelayMS > 0 {
		timer := time.NewTimer(time.Duration(b.DelayMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}
	status := b.Status
	if len(b.Statuses) > 0 {
		status = b.Statuses[min(index, len(b.Statuses)-1)]
	}
	if status == 0 {
		status = 200
	}
	respond(w, status, entry)
}

func (f *fixture) control(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "GET" && r.URL.Path == "/state":
		respond(w, 200, map[string]any{"count": f.count, "active": f.active, "requests": f.records})
	case r.Method == "POST" && r.URL.Path == "/reset":
		f.count, f.records, f.behavior = 0, nil, behavior{}
		respond(w, 200, map[string]bool{"ok": true})
	case r.Method == "PUT" && r.URL.Path == "/behavior":
		var b behavior
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&b); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		if b.DelayMS < 0 || b.DelayMS > 60000 || b.Status != 0 && (b.Status < 200 || b.Status > 599) {
			http.Error(w, "invalid behavior", 400)
			return
		}
		for _, status := range b.Statuses {
			if status < 200 || status > 599 {
				http.Error(w, "invalid status", 400)
				return
			}
		}
		f.behavior = b
		respond(w, 200, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "ctl" {
		body, err := io.ReadAll(io.LimitReader(os.Stdin, 65537))
		if err != nil {
			log.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, os.Args[2], "http://127.0.0.1:9091"+os.Args[3], bytes.NewReader(body))
		if err != nil {
			log.Fatal(err)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(os.Stdout, response.Body)
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	f := &fixture{name: os.Getenv("NAME")}
	errs := make(chan error, 2)
	for address, handler := range map[string]http.HandlerFunc{":8080": f.serve, ":9091": f.control} {
		server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 65 * time.Second}
		go func() { errs <- server.ListenAndServe() }()
	}
	_, _ = fmt.Fprintln(os.Stderr, <-errs)
	os.Exit(1)
}
