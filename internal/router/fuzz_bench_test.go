package router

import (
	"net/http/httptest"
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
		request := httptest.NewRequest("GET", "http://example.invalid"+path, nil)
		request.Host = host
		_, _ = matcher.Match(request)
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
