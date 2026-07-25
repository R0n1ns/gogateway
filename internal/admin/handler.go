// Package admin предоставляет control API и небольшой web-интерфейс.
package admin

import (
	"crypto/subtle"
	"encoding/json"
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"net/http"
	"strings"
)

type Handler struct {
	runtime *gateway.Runtime
	apiKey  string
	mux     *http.ServeMux
}

func New(g *gateway.Runtime, key string) *Handler {
	h := &Handler{runtime: g, apiKey: key, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	h.mux.Handle("GET /metrics", g.Metrics())
	h.mux.HandleFunc("GET /", ui)
	h.mux.HandleFunc("GET /api/v1/config", h.get)
	h.mux.HandleFunc("PUT /api/v1/config", h.put)
	h.mux.HandleFunc("GET /api/v1/policies", h.policies)
	h.mux.HandleFunc("POST /api/v1/policies", h.add)
	h.mux.HandleFunc("DELETE /api/v1/policies/{id}", h.remove)
	return h
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") && h.apiKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(h.apiKey)) != 1 {
		write(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) get(w http.ResponseWriter, _ *http.Request) { write(w, 200, h.runtime.Config()) }
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	var c config.Config
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&c) != nil {
		write(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if e := h.runtime.Update(&c); e != nil {
		write(w, 422, map[string]string{"error": e.Error()})
		return
	}
	write(w, 200, h.runtime.Config())
}
func (h *Handler) policies(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, h.runtime.Config().Policies)
}
func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var p config.Policy
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		write(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	c := h.runtime.Config()
	for _, v := range c.Policies {
		if v.ID == p.ID {
			write(w, 409, map[string]string{"error": "policy already exists"})
			return
		}
	}
	c.Policies = append(c.Policies, p)
	if e := h.runtime.Update(c); e != nil {
		write(w, 422, map[string]string{"error": e.Error()})
		return
	}
	write(w, 201, p)
}
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	c := h.runtime.Config()
	out := c.Policies[:0]
	found := false
	for _, p := range c.Policies {
		if p.ID == r.PathValue("id") {
			found = true
		} else {
			out = append(out, p)
		}
	}
	if !found {
		write(w, 404, map[string]string{"error": "not found"})
		return
	}
	c.Policies = out
	_ = h.runtime.Update(c)
	w.WriteHeader(204)
}
func write(w http.ResponseWriter, s int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s)
	_ = json.NewEncoder(w).Encode(v)
}
func ui(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset=utf-8><title>GateForge</title><style>body{font:16px system-ui;max-width:960px;margin:40px auto;background:#101827;color:#e5e7eb}pre{background:#1f2937;padding:20px;overflow:auto}button{padding:8px 14px}</style></head><body><h1>GateForge</h1><p>Application Gateway with Policy Engine</p><button onclick="load()">Обновить</button><pre id=o>Загрузка…</pre><script>async function load(){o.textContent=JSON.stringify(await (await fetch('/api/v1/config')).json(),null,2)}load()</script></body></html>`))
}
