package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/control"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/storage"
)

func TestAdminAuthenticationAndAudit(t *testing.T) {
	handler, _, service, cleanup := testHandler(t)
	defer cleanup()
	request := httptest.NewRequest("GET", "/api/v1/config", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
	events, err := service.Audit(t.Context(), 10)
	if err != nil || len(events) != 1 || events[0].Action != "auth.denied" {
		t.Fatalf("audit=%+v err=%v", events, err)
	}
}

func TestConfigVersionsConflictAndRollback(t *testing.T) {
	handler, runtime, _, cleanup := testHandler(t)
	defer cleanup()
	cfg := runtime.Config()
	cfg.AccessLog.Enabled = true
	body, _ := json.Marshal(cfg)
	response := doAdmin(handler, "PUT", "/api/v1/config", body, map[string]string{"If-Match": "1", "X-Change-Comment": "enable log"})
	if response.Code != 200 || runtime.Config().Revision != 2 {
		t.Fatalf("apply status=%d body=%s revision=%d", response.Code, response.Body.String(), runtime.Config().Revision)
	}
	stale := doAdmin(handler, "PUT", "/api/v1/config", body, map[string]string{"If-Match": "1"})
	if stale.Code != 409 {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	rollback := doAdmin(handler, "POST", "/api/v1/rollback/1", nil, map[string]string{"If-Match": "2"})
	if rollback.Code != 200 || runtime.Config().Revision != 3 || runtime.Config().AccessLog.Enabled {
		t.Fatalf("rollback status=%d cfg=%+v", rollback.Code, runtime.Config().AccessLog)
	}
}

func TestSimulatorAndUpstreamEndpoints(t *testing.T) {
	handler, _, _, cleanup := testHandler(t)
	defer cleanup()
	simulation := []byte(`{"method":"DELETE","host":"example","path":"/api/users","source_ip":"203.0.113.1"}`)
	response := doAdmin(handler, "POST", "/api/v1/simulate", simulation, nil)
	if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"route_id":"api"`)) {
		t.Fatalf("simulate=%d %s", response.Code, response.Body.String())
	}
	upstreams := doAdmin(handler, "GET", "/api/v1/upstreams", nil, nil)
	if upstreams.Code != 200 || !bytes.Contains(upstreams.Body.Bytes(), []byte(`"backend"`)) {
		t.Fatalf("upstreams=%d %s", upstreams.Code, upstreams.Body.String())
	}
}

func testHandler(t *testing.T) (*Handler, *gateway.Runtime, *control.Service, func()) {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	cfg := &config.Config{Version: 1, Revision: 1, Public: config.Server{Address: ":8080"}, Admin: config.AdminServer{Server: config.Server{Address: ":9090"}, APIKey: "secret"}, Storage: config.Storage{Driver: "memory"}, Routes: []config.Route{{ID: "api", PathPrefix: "/api", UpstreamPool: []config.Upstream{{ID: "backend", URL: backend.URL, Weight: 1}}, Balance: config.Balance{Strategy: "round_robin"}, Retry: config.Retry{Attempts: 1}}}, Policies: []config.Policy{{ID: "deny-delete", Mode: config.ModeBlock, Action: config.ActionDeny, Match: config.Match{Methods: []string{"DELETE"}}}}}
	cfg.Defaults()
	cfg.Normalize()
	runtime, err := gateway.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	repo := storage.NewMemory(cfg)
	service := control.New(runtime, repo)
	handler := NewWithControl(runtime, service, "secret")
	return handler, runtime, service, func() { runtime.Close(); _ = service.Close(); backend.Close() }
}
func doAdmin(handler http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("X-API-Key", "secret")
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestListEndpointsDoNotExposeResolvedSecrets(t *testing.T) {
	handler, runtime, service, cleanup := testHandler(t)
	defer cleanup()

	cfg := runtime.Config()
	backendURL := cfg.Routes[0].UpstreamPool[0].URL
	t.Setenv("BACKEND_URL", backendURL)
	t.Setenv("SERVICE_KEY", "resolved-service-secret")
	cfg.Routes[0].UpstreamPool[0].URL = "${BACKEND_URL}"
	cfg.Policies = append(cfg.Policies, config.Policy{
		ID: "auth", Priority: 20, Mode: config.ModeBlock, Action: config.ActionAuthenticate,
		Auth: &config.Auth{Type: "api_key", Header: "X-Service-Key", Value: "${SERVICE_KEY}"},
	})
	if _, err := service.Apply(t.Context(), cfg, cfg.Revision, "test", "secret placeholders"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/v1/routes", "/api/v1/policies"} {
		response := doAdmin(handler, "GET", path, nil, nil)
		body := response.Body.String()
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, body)
		}
		if bytes.Contains([]byte(body), []byte("resolved-service-secret")) || bytes.Contains([]byte(body), []byte(backendURL)) {
			t.Fatalf("%s раскрыл resolved secret: %s", path, body)
		}
	}
}
