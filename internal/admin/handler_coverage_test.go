package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestReadOnlyAndOperationalEndpoints(t *testing.T) {
	handler, runtime, _, cleanup := testHandler(t)
	defer cleanup()

	cases := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/healthz", http.StatusOK, `"status":"ok"`},
		{"/readyz", http.StatusOK, `"status":"ready"`},
		{"/", http.StatusOK, "GateForge"},
		{"/api/v1/config", http.StatusOK, `"revision":1`},
		{"/api/v1/policies", http.StatusOK, `"deny-delete"`},
		{"/api/v1/routes", http.StatusOK, `"api"`},
		{"/api/v1/versions?limit=bad", http.StatusOK, `"revision":1`},
		{"/api/v1/versions/1", http.StatusOK, `"revision":1`},
		{"/api/v1/audit?limit=bad", http.StatusOK, `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := doAdmin(handler, http.MethodGet, tc.path, nil, nil)
			if response.Code != tc.wantStatus || !bytes.Contains(response.Body.Bytes(), []byte(tc.wantBody)) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if runtime.Metrics() == nil {
		t.Fatal("metrics handler не создан")
	}
}

func TestPolicyCRUDAndValidationErrors(t *testing.T) {
	handler, _, _, cleanup := testHandler(t)
	defer cleanup()

	invalid := doAdmin(handler, http.MethodPost, "/api/v1/policies", []byte(`{"id":`), nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", invalid.Code)
	}
	policy := config.Policy{ID: "shadow", Priority: 20, Mode: config.ModeShadow, Action: config.ActionDeny}
	body, _ := json.Marshal(policy)
	created := doAdmin(handler, http.MethodPost, "/api/v1/policies", body, map[string]string{"X-Actor": "tester"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	duplicate := doAdmin(handler, http.MethodPost, "/api/v1/policies", body, nil)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d", duplicate.Code)
	}
	missing := doAdmin(handler, http.MethodDelete, "/api/v1/policies/missing", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
	deleted := doAdmin(handler, http.MethodDelete, "/api/v1/policies/shadow", nil, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestRouteCRUDAndInvalidPayloads(t *testing.T) {
	handler, runtime, _, cleanup := testHandler(t)
	defer cleanup()

	invalid := doAdmin(handler, http.MethodPost, "/api/v1/routes", []byte(`{`), nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", invalid.Code)
	}
	route := runtime.Config().Routes[0]
	route.ID = "extra"
	route.PathPrefix = "/extra"
	body, _ := json.Marshal(route)
	created := doAdmin(handler, http.MethodPost, "/api/v1/routes", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	duplicate := doAdmin(handler, http.MethodPost, "/api/v1/routes", body, nil)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d", duplicate.Code)
	}
	missing := doAdmin(handler, http.MethodDelete, "/api/v1/routes/missing", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
	deleted := doAdmin(handler, http.MethodDelete, "/api/v1/routes/extra", nil, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestConfigAndSimulatorRejectInvalidJSON(t *testing.T) {
	handler, _, _, cleanup := testHandler(t)
	defer cleanup()

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/config"},
		{http.MethodPost, "/api/v1/simulate"},
	} {
		response := doAdmin(handler, tc.method, tc.path, []byte(`{"broken"`), nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestNewCreatesStandaloneHandler(t *testing.T) {
	_, runtime, _, cleanup := testHandler(t)
	defer cleanup()
	handler := New(runtime, "standalone")
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
}
