package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func FuzzRouteMatch(f *testing.F) {
	f.Add("example.com", "/api/users", "/api")
	f.Add("localhost", "/", "/")
	f.Fuzz(func(t *testing.T, host, path, prefix string) {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		if len(path) > 2048 || len(prefix) > 256 {
			return
		}
		matcher := New([]config.Route{{ID: "route", Host: strings.ToLower(host), PathPrefix: prefix}})
		request := &http.Request{Method: "GET", URL: &url.URL{Path: path}, Host: host}
		got, ok := matcher.Match(request)
		// Host is tested independently below; use a wildcard for arbitrary bytes.
		wildcard := New([]config.Route{{ID: "wildcard", PathPrefix: prefix}})
		_, matchesPath := wildcard.Match(request)
		want := prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/")
		if matchesPath != want {
			t.Fatalf("path=%q prefix=%q got=%v want=%v", path, prefix, matchesPath, want)
		}
		if ok && (got.ID != "route" || !matchesPath) {
			t.Fatal("invalid route selection")
		}
		request.Host = "EXAMPLE.COM"
		fixed := New([]config.Route{{ID: "fixed", Host: "example.com", PathPrefix: "/"}})
		if _, matched := fixed.Match(request); !matched {
			t.Fatal("host matching must ignore case")
		}
	})
}
func BenchmarkMatch1000Routes(b *testing.B) {
	routes := make([]config.Route, 1000)
	for i := range routes {
		routes[i] = config.Route{ID: "route", PathPrefix: "/api"}
	}
	matcher := New(routes)
	request := httptest.NewRequest("GET", "http://example/api/users", nil)
	b.ReportAllocs()
	for b.Loop() {
		matcher.Match(request)
	}
}
