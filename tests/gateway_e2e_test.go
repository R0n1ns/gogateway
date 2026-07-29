package tests

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWeightedGatewayEndToEnd(t *testing.T) {
	var first, second atomic.Int32
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { first.Add(1); w.WriteHeader(204) }))
	defer one.Close()
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { second.Add(1); w.WriteHeader(204) }))
	defer two.Close()
	cfg := &config.Config{Version: 1, Public: config.Server{Address: ":8080"}, Admin: config.AdminServer{Server: config.Server{Address: ":9090"}}, Storage: config.Storage{Driver: "memory"}, Routes: []config.Route{{ID: "api", PathPrefix: "/api", UpstreamPool: []config.Upstream{{ID: "one", URL: one.URL, Weight: 3}, {ID: "two", URL: two.URL, Weight: 1}}, Balance: config.Balance{Strategy: "weighted_round_robin"}, Retry: config.Retry{Attempts: 1}}}}
	cfg.Defaults()
	cfg.Normalize()
	runtime, err := gateway.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for range 40 {
		response := httptest.NewRecorder()
		runtime.ServeHTTP(response, httptest.NewRequest("GET", "http://gateway/api", nil))
		if response.Code != 204 {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if first.Load() != 30 || second.Load() != 10 {
		t.Fatalf("distribution=%d/%d", first.Load(), second.Load())
	}
}
