package control

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/storage"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyConflictAndRollback(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer backend.Close()
	initial := controlConfig(backend.URL)
	runtime, err := gateway.New(initial, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	repo := storage.NewMemory(initial)
	service := New(runtime, repo)
	changed := initial.DeepCopy()
	changed.AccessLog.Enabled = true
	snapshot, err := service.Apply(t.Context(), changed, 1, "admin", "enable logs")
	if err != nil || snapshot.Revision != 2 || runtime.Config().Revision != 2 {
		t.Fatalf("snapshot=%+v runtime=%d err=%v", snapshot, runtime.Config().Revision, err)
	}
	if _, err = service.Apply(t.Context(), changed, 1, "admin", "stale"); err == nil {
		t.Fatal("stale update принят")
	}
	rolled, err := service.Rollback(t.Context(), 1, 2, "admin")
	if err != nil || rolled.Revision != 3 || runtime.Config().AccessLog.Enabled {
		t.Fatalf("rollback=%+v err=%v", rolled, err)
	}
}
func controlConfig(backend string) *config.Config {
	cfg := &config.Config{Version: 1, Revision: 1, Public: config.Server{Address: ":8080"}, Admin: config.AdminServer{Server: config.Server{Address: ":9090"}}, Storage: config.Storage{Driver: "memory"}, Routes: []config.Route{{ID: "api", PathPrefix: "/", UpstreamPool: []config.Upstream{{ID: "one", URL: backend, Weight: 1}}, Balance: config.Balance{Strategy: "round_robin"}, Retry: config.Retry{Attempts: 1}}}}
	cfg.Defaults()
	cfg.Normalize()
	return cfg
}
