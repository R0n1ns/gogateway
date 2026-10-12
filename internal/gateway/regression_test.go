package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChunkedBodyLimit(t *testing.T) {
	var calls atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer backend.Close()
	g := testRuntime(t, backend.URL, nil)
	defer g.Close()
	r := httptest.NewRequest("POST", "/api", strings.NewReader("12345"))
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 413 || calls.Load() != 0 {
		t.Fatalf("status=%d backend calls=%d", w.Code, calls.Load())
	}
}

func TestUntrustedForwardedFor(t *testing.T) {
	var forwarded string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(200)
	}))
	defer backend.Close()
	g := testRuntime(t, backend.URL, nil)
	defer g.Close()
	r := httptest.NewRequest("GET", "/api", nil)
	r.RemoteAddr = "192.0.2.4:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.99")
	g.ServeHTTP(httptest.NewRecorder(), r)
	if forwarded != "192.0.2.4" {
		t.Fatalf("spoofed forwarding context: %s", forwarded)
	}
}
