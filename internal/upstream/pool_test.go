package upstream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestWeightedRoundRobin(t *testing.T) {
	route := testRoute("weighted_round_robin", []config.Upstream{{ID: "a", URL: "http://a", Weight: 3}, {ID: "b", URL: "http://b", Weight: 1}})
	pool, err := New(route)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	counts := map[string]int{}
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	for range 40 {
		selection, selectErr := pool.Select(request)
		if selectErr != nil {
			t.Fatal(selectErr)
		}
		counts[selection.Backend.ID()]++
		selection.Release()
	}
	if counts["a"] != 30 || counts["b"] != 10 {
		t.Fatalf("weights=%v", counts)
	}
}

func TestRejectInvalidWeightTotals(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, weights := range [][]int{{0}, {-1}, {maxInt, 1}} {
		items := make([]config.Upstream, len(weights))
		for i, weight := range weights {
			items[i] = config.Upstream{ID: "test", URL: "http://example.test", Weight: weight}
		}
		if pool, err := New(testRoute("weighted_round_robin", items)); err == nil {
			pool.Close()
			t.Fatalf("accepted invalid weights: %v", weights)
		}
	}
}

func TestRandomSelectionUsesOnlyConfiguredBackends(t *testing.T) {
	pool, err := New(testRoute("random", []config.Upstream{{ID: "a", URL: "http://a", Weight: 1}, {ID: "b", URL: "http://b", Weight: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for range 20 {
		selection, err := pool.Select(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		if id := selection.Backend.ID(); id != "a" && id != "b" {
			t.Fatalf("unexpected backend %q", id)
		}
		selection.Release()
	}
}

func TestLeastConnections(t *testing.T) {
	route := testRoute("least_connections", []config.Upstream{{ID: "a", URL: "http://a", Weight: 1}, {ID: "b", URL: "http://b", Weight: 1}})
	pool, _ := New(route)
	defer pool.Close()
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	first, _ := pool.Select(request)
	second, _ := pool.Select(request)
	if first.Backend.ID() == second.Backend.ID() {
		t.Fatalf("выбран занятый backend %s", first.Backend.ID())
	}
	first.Release()
	second.Release()
}

func TestConsistentHashIsStable(t *testing.T) {
	route := testRoute("consistent_hash", []config.Upstream{{ID: "a", URL: "http://a", Weight: 1}, {ID: "b", URL: "http://b", Weight: 1}})
	route.Balance.HashKey = "header:X-Tenant"
	pool, _ := New(route)
	defer pool.Close()
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	request.Header.Set("X-Tenant", "tenant-42")
	first, _ := pool.Select(request)
	id := first.Backend.ID()
	first.Release()
	for range 10 {
		next, _ := pool.Select(request)
		if next.Backend.ID() != id {
			t.Fatal("hash selection нестабилен")
		}
		next.Release()
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	route := testRoute("round_robin", []config.Upstream{{ID: "a", URL: "http://a", Weight: 1}})
	route.CircuitBreaker = config.CircuitBreaker{FailureThreshold: 1, OpenTimeout: config.Duration{Duration: time.Hour}, HalfOpenRequests: 1}
	pool, _ := New(route)
	defer pool.Close()
	request := httptest.NewRequest("GET", "http://gateway/", nil)
	selection, _ := pool.Select(request)
	pool.ObserveResult(selection.Backend, 0, errors.New("network"))
	selection.Release()
	if _, err := pool.Select(request); !errors.Is(err, ErrNoHealthyUpstream) {
		t.Fatalf("Select() error=%v", err)
	}
}

func TestActiveHealthCheck(t *testing.T) {
	status := http.StatusServiceUnavailable
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer server.Close()
	route := testRoute("round_robin", []config.Upstream{{ID: "a", URL: server.URL, Weight: 1}})
	route.HealthCheck = config.HealthCheck{Enabled: true, Path: "/health", Interval: config.Duration{Duration: time.Hour}, Timeout: config.Duration{Duration: time.Second}, HealthyThreshold: 1, UnhealthyThreshold: 1}
	pool, _ := New(route)
	defer pool.Close()
	pool.check(t.Context(), pool.backends[0])
	if pool.backends[0].Healthy() {
		t.Fatal("unhealthy backend оставлен активным")
	}
	status = http.StatusOK
	pool.check(t.Context(), pool.backends[0])
	if !pool.backends[0].Healthy() {
		t.Fatal("backend не восстановлен")
	}
}

func testRoute(strategy string, backends []config.Upstream) config.Route {
	return config.Route{ID: "route", UpstreamPool: backends, Balance: config.Balance{Strategy: strategy}, UpstreamTimeout: config.Duration{Duration: time.Second}, Retry: config.Retry{Attempts: 1}}
}

func TestStatusHidesURLCredentialsAndPath(t *testing.T) {
	route := testRoute("round_robin", []config.Upstream{{ID: "private", URL: "http://user:password@example.test/private?token=secret", Weight: 1}})
	pool, err := New(route)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	statuses := pool.Status()
	if len(statuses) != 1 || statuses[0].URL != "http://example.test" {
		t.Fatalf("небезопасный status URL: %+v", statuses)
	}
}
