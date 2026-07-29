package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestClientIPTrustBoundary(t *testing.T) {
	trusted, _ := NewTrustedProxies([]string{"10.0.0.0/8"})
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	request.RemoteAddr = "203.0.113.5:1234"
	request.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(request, trusted); got != "203.0.113.5" {
		t.Fatalf("подделанный XFF принят: %s", got)
	}
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.3")
	if got := ClientIP(request, trusted); got != "198.51.100.7" {
		t.Fatalf("client IP=%s", got)
	}
}

func TestRetryAndResponseTransformation(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()
	cfg := runtimeV2Config(backend.URL)
	cfg.Routes[0].Retry = config.Retry{Attempts: 2, Backoff: config.Duration{Duration: time.Millisecond}, Statuses: []int{503}, Methods: []string{"GET"}}
	cfg.Routes[0].AddResponseHeaders = map[string]string{"X-Gateway-Response": "yes"}
	runtime, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	response := httptest.NewRecorder()
	runtime.ServeHTTP(response, httptest.NewRequest("GET", "http://gateway/api", nil))
	if response.Code != 200 || calls.Load() != 2 || response.Header().Get("X-Gateway-Response") != "yes" {
		t.Fatalf("status=%d calls=%d headers=%v", response.Code, calls.Load(), response.Header())
	}
}

func TestRateLimitHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	cfg := runtimeV2Config(backend.URL)
	cfg.Policies = []config.Policy{{ID: "limit", Mode: config.ModeBlock, Action: config.ActionRateLimit, Limit: &config.Limit{Requests: 1, Window: config.Duration{Duration: time.Minute}, Key: "ip"}}}
	runtime, _ := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer runtime.Close()
	first := httptest.NewRecorder()
	runtime.ServeHTTP(first, httptest.NewRequest("GET", "http://gateway/api", nil))
	second := httptest.NewRecorder()
	runtime.ServeHTTP(second, httptest.NewRequest("GET", "http://gateway/api", nil))
	if first.Header().Get("RateLimit-Limit") != "1" || second.Code != 429 || second.Header().Get("RateLimit-Remaining") != "0" {
		t.Fatalf("first=%v second=%d %v", first.Header(), second.Code, second.Header())
	}
}

func TestW3CTracePropagation(t *testing.T) {
	tracer := NewW3CTracer()
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	request.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	updated, span := tracer.Start(request)
	if span.TraceID() != "0123456789abcdef0123456789abcdef" || !strings.HasPrefix(updated.Header.Get("Traceparent"), "00-0123456789abcdef0123456789abcdef-") {
		t.Fatalf("trace=%s parent=%s", span.TraceID(), updated.Header.Get("Traceparent"))
	}
}

func TestMetricsEscapeLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveRequest(`route"bad`, `allow`, 200, time.Millisecond)
	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(response.Body.String(), `route="route\"bad"`) {
		t.Fatalf("label не экранирован: %s", response.Body.String())
	}
}

func runtimeV2Config(backend string) *config.Config {
	cfg := &config.Config{Version: 1, Public: config.Server{Address: ":8080"}, Admin: config.AdminServer{Server: config.Server{Address: ":9090"}}, Storage: config.Storage{Driver: "memory"}, Routes: []config.Route{{ID: "api", PathPrefix: "/api", UpstreamPool: []config.Upstream{{ID: "backend", URL: backend, Weight: 1}}, Balance: config.Balance{Strategy: "round_robin"}, Retry: config.Retry{Attempts: 1}}}}
	cfg.Defaults()
	cfg.Normalize()
	return cfg
}
