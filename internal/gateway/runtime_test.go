package gateway

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testRuntime(t *testing.T, up string, policies []config.Policy) *Runtime {
	t.Helper()
	c := &config.Config{Version: 1, Public: config.Server{Address: ":1"}, Admin: config.AdminServer{Server: config.Server{Address: ":2"}}, Routes: []config.Route{{ID: "api", PathPrefix: "/api", Upstreams: []string{up}, MaxBodyBytes: 4, AddHeaders: map[string]string{"X-Gateway": "yes"}}}, Policies: policies}
	g, e := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func TestProxyAndHeaders(t *testing.T) {
	back := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gateway") != "yes" {
			t.Error("нет заголовка")
		}
		w.WriteHeader(201)
	}))
	defer back.Close()
	g := testRuntime(t, back.URL, nil)
	r := httptest.NewRequest("GET", "http://x/api/users", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 201 || w.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
}
func TestDenyLimitAndBody(t *testing.T) {
	ps := []config.Policy{{ID: "deny", Mode: "block", Action: "deny", Match: config.Match{Methods: []string{"DELETE"}}}, {ID: "limit", Mode: "block", Action: "rate_limit", Match: config.Match{Methods: []string{"GET"}}, Limit: &config.Limit{Requests: 1, Window: config.Duration{Duration: 1000000000}}}}
	g := testRuntime(t, "http://127.0.0.1:1", ps)
	for _, tc := range []struct {
		method, body string
		want         int
	}{{"DELETE", "", 403}, {"POST", "12345", 413}, {"GET", "", 502}, {"GET", "", 429}} {
		r := httptest.NewRequest(tc.method, "http://x/api", strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s status=%d want=%d body=%s", tc.method, w.Code, tc.want, w.Body.String())
		}
	}
}
func TestHotUpdate(t *testing.T) {
	g := testRuntime(t, "http://127.0.0.1:1", nil)
	c := g.Config()
	c.Policies = []config.Policy{{ID: "all", Mode: "block", Action: "deny"}}
	if err := g.Update(c); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("GET", "http://x/api", nil))
	if w.Code != 403 {
		t.Fatalf("status=%d", w.Code)
	}
}
