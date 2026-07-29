// Package admin реализует HTTP control plane GateForge.
package admin

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/control"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/simulator"
	"github.com/R0n1ns/gogateway/internal/storage"
)

//go:embed ui.html
var assets embed.FS

type Handler struct {
	runtime DataPlane
	control ControlPlane
	apiKey  string
	mux     *http.ServeMux
}

func New(runtime *gateway.Runtime, key string) *Handler {
	return NewWithControl(runtime, control.New(runtime, storage.NewMemory(runtime.Config())), key)
}
func NewWithControl(runtime DataPlane, service ControlPlane, key string) *Handler {
	h := &Handler{runtime: runtime, control: service, apiKey: key, mux: http.NewServeMux()}
	h.routes()
	return h
}
func (h *Handler) routes() {
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	h.mux.HandleFunc("GET /readyz", h.ready)
	h.mux.Handle("GET /metrics", h.runtime.Metrics())
	h.mux.HandleFunc("GET /", h.ui)
	h.mux.HandleFunc("GET /api/v1/config", h.getConfig)
	h.mux.HandleFunc("PUT /api/v1/config", h.putConfig)
	h.mux.HandleFunc("GET /api/v1/policies", h.getPolicies)
	h.mux.HandleFunc("POST /api/v1/policies", h.addPolicy)
	h.mux.HandleFunc("DELETE /api/v1/policies/{id}", h.deletePolicy)
	h.mux.HandleFunc("GET /api/v1/routes", h.getRoutes)
	h.mux.HandleFunc("POST /api/v1/routes", h.addRoute)
	h.mux.HandleFunc("DELETE /api/v1/routes/{id}", h.deleteRoute)
	h.mux.HandleFunc("GET /api/v1/versions", h.versions)
	h.mux.HandleFunc("GET /api/v1/versions/{revision}", h.version)
	h.mux.HandleFunc("POST /api/v1/rollback/{revision}", h.rollback)
	h.mux.HandleFunc("GET /api/v1/audit", h.audit)
	h.mux.HandleFunc("POST /api/v1/simulate", h.simulate)
	h.mux.HandleFunc("GET /api/v1/upstreams", h.upstreams)
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") && h.apiKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(h.apiKey)) != 1 {
		_ = h.control.RecordAudit(r.Context(), storage.AuditEvent{Action: "auth.denied", Actor: actor(r), Details: r.Method + " " + r.URL.Path})
		adminError(w, http.StatusUnauthorized, "unauthorized", "неверный API key")
		return
	}
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) ready(w http.ResponseWriter, _ *http.Request) {
	if !h.runtime.Ready() {
		write(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	write(w, 200, map[string]string{"status": "ready"})
}
func (h *Handler) getConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := h.runtime.Config()
	w.Header().Set("ETag", strconv.FormatInt(cfg.Revision, 10))
	write(w, 200, cfg)
}
func (h *Handler) putConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := decode(w, r, &cfg, h.runtime.Config().Runtime.MaxConfigBytes); err != nil {
		adminError(w, 400, "invalid_json", err.Error())
		return
	}
	snapshot, err := h.control.Apply(r.Context(), &cfg, expectedRevision(r, h.runtime.Config().Revision), actor(r), comment(r))
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("ETag", strconv.FormatInt(snapshot.Revision, 10))
	write(w, 200, snapshot)
}
func (h *Handler) getPolicies(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, h.runtime.Config().Policies)
}
func (h *Handler) addPolicy(w http.ResponseWriter, r *http.Request) {
	var policy config.Policy
	if err := decode(w, r, &policy, 256<<10); err != nil {
		adminError(w, 400, "invalid_json", err.Error())
		return
	}
	cfg := h.runtime.Config()
	for _, current := range cfg.Policies {
		if current.ID == policy.ID {
			adminError(w, 409, "already_exists", "policy уже существует")
			return
		}
	}
	cfg.Policies = append(cfg.Policies, policy)
	snapshot, err := h.control.Apply(r.Context(), cfg, expectedRevision(r, cfg.Revision), actor(r), "add policy "+policy.ID)
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 201, snapshot)
}
func (h *Handler) deletePolicy(w http.ResponseWriter, r *http.Request) {
	cfg := h.runtime.Config()
	out := cfg.Policies[:0]
	found := false
	for _, item := range cfg.Policies {
		if item.ID == r.PathValue("id") {
			found = true
		} else {
			out = append(out, item)
		}
	}
	if !found {
		adminError(w, 404, "not_found", "policy не найдена")
		return
	}
	cfg.Policies = out
	if _, err := h.control.Apply(r.Context(), cfg, expectedRevision(r, cfg.Revision), actor(r), "delete policy "+r.PathValue("id")); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) getRoutes(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, h.runtime.Config().Routes)
}
func (h *Handler) addRoute(w http.ResponseWriter, r *http.Request) {
	var route config.Route
	if err := decode(w, r, &route, 256<<10); err != nil {
		adminError(w, 400, "invalid_json", err.Error())
		return
	}
	cfg := h.runtime.Config()
	for _, current := range cfg.Routes {
		if current.ID == route.ID {
			adminError(w, 409, "already_exists", "route уже существует")
			return
		}
	}
	cfg.Routes = append(cfg.Routes, route)
	snapshot, err := h.control.Apply(r.Context(), cfg, expectedRevision(r, cfg.Revision), actor(r), "add route "+route.ID)
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 201, snapshot)
}
func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	cfg := h.runtime.Config()
	out := cfg.Routes[:0]
	found := false
	for _, item := range cfg.Routes {
		if item.ID == r.PathValue("id") {
			found = true
		} else {
			out = append(out, item)
		}
	}
	if !found {
		adminError(w, 404, "not_found", "route не найден")
		return
	}
	cfg.Routes = out
	if _, err := h.control.Apply(r.Context(), cfg, expectedRevision(r, cfg.Revision), actor(r), "delete route "+r.PathValue("id")); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) versions(w http.ResponseWriter, r *http.Request) {
	limit := parseInt(r.URL.Query().Get("limit"), 50)
	items, err := h.control.List(r.Context(), limit)
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 200, items)
}
func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	revision := parseInt64(r.PathValue("revision"))
	item, err := h.control.Get(r.Context(), revision)
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 200, item)
}
func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	target := parseInt64(r.PathValue("revision"))
	snapshot, err := h.control.Rollback(r.Context(), target, expectedRevision(r, h.runtime.Config().Revision), actor(r))
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 200, snapshot)
}
func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	items, err := h.control.Audit(r.Context(), parseInt(r.URL.Query().Get("limit"), 100))
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 200, items)
}
func (h *Handler) simulate(w http.ResponseWriter, r *http.Request) {
	var input simulator.Request
	if err := decode(w, r, &input, 128<<10); err != nil {
		adminError(w, 400, "invalid_json", err.Error())
		return
	}
	service, err := simulator.New(h.runtime.Config())
	if err != nil {
		handleError(w, err)
		return
	}
	write(w, 200, service.Simulate(input))
}
func (h *Handler) upstreams(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, h.runtime.UpstreamStatus())
}
func (h *Handler) ui(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.ReadFile("ui.html")
	if err != nil {
		http.Error(w, "UI unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
func decode(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes)).Decode(target)
}
func expectedRevision(r *http.Request, fallback int64) int64 {
	value := strings.Trim(r.Header.Get("If-Match"), `"`)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
func actor(r *http.Request) string {
	if value := r.Header.Get("X-Actor"); value != "" {
		return value
	}
	return "api"
}
func comment(r *http.Request) string { return r.Header.Get("X-Change-Comment") }
func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
func parseInt64(value string) int64 { parsed, _ := strconv.ParseInt(value, 10, 64); return parsed }
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func adminError(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]any{"code": code, "message": message})
}
func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrConflict):
		adminError(w, 409, "revision_conflict", "конфигурация была изменена другим оператором")
	case errors.Is(err, storage.ErrNotFound):
		adminError(w, 404, "not_found", "snapshot не найден")
	default:
		adminError(w, 422, "operation_failed", err.Error())
	}
}
