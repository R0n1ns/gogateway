// Package gateway соединяет маршрутизацию, политики и reverse proxy.
package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/policy"
	"github.com/R0n1ns/gogateway/internal/ratelimit"
	"github.com/R0n1ns/gogateway/internal/router"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type state struct {
	cfg    *config.Config
	router *router.Router
	engine *policy.Engine
}
type Runtime struct {
	current atomic.Pointer[state]
	limiter *ratelimit.Limiter
	logger  *slog.Logger
	metrics *Metrics
	rr      atomic.Uint64
}

func New(c *config.Config, l *slog.Logger) (*Runtime, error) {
	g := &Runtime{limiter: ratelimit.New(), logger: l, metrics: NewMetrics()}
	if err := g.Update(c); err != nil {
		return nil, err
	}
	return g, nil
}
func (g *Runtime) Config() *config.Config { return clone(g.current.Load().cfg) }
func (g *Runtime) Update(c *config.Config) error {
	copy := clone(c)
	copy.Defaults()
	if err := copy.Validate(); err != nil {
		return err
	}
	copy.Normalize()
	g.current.Store(&state{cfg: copy, router: router.New(copy.Routes), engine: policy.New(copy.Policies)})
	g.limiter.Reset()
	return nil
}
func (g *Runtime) Metrics() http.Handler { return g.metrics }
func (g *Runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := requestID()
	w.Header().Set("X-Request-ID", id)
	rw := &recorder{ResponseWriter: w, status: 200}
	routeID := ""
	decision := "allow"
	defer func() {
		g.metrics.Observe(routeID, decision, rw.status)
		if g.current.Load().cfg.AccessLog.Enabled {
			g.logger.Info("access", "request_id", id, "method", r.Method, "path", r.URL.Path, "route", routeID, "decision", decision, "status", rw.status, "duration_ms", time.Since(start).Milliseconds(), "source_ip", clientIP(r))
		}
	}()
	s := g.current.Load()
	route, ok := s.router.Match(r)
	if !ok {
		decision = "route_not_found"
		writeJSON(rw, 404, map[string]string{"error": "route not found", "request_id": id})
		return
	}
	routeID = route.ID
	if route.MaxBodyBytes > 0 {
		if r.ContentLength > route.MaxBodyBytes {
			decision = "body_too_large"
			writeJSON(rw, 413, map[string]string{"error": "request body too large", "request_id": id})
			return
		}
		r.Body = http.MaxBytesReader(rw, r.Body, route.MaxBodyBytes)
	}
	d := s.engine.Evaluate(r, route.ID, clientIP(r))
	for _, v := range d.Shadow {
		g.logger.Warn("shadow policy matched", "request_id", id, "policy_id", v)
		g.metrics.Shadow(v)
	}
	if !d.Allowed {
		decision = "deny"
		writeJSON(rw, 403, map[string]string{"error": "request denied", "policy_id": d.PolicyID, "request_id": id})
		return
	}
	if d.Limit != nil {
		key := limitKey(r, d.Limit)
		if !g.limiter.Allow(d.PolicyID+":"+key, d.Limit.Requests, d.Limit.Burst, d.Limit.Window.Duration) {
			decision = "rate_limit"
			rw.Header().Set("Retry-After", strconv.Itoa(int(d.Limit.Window.Seconds())))
			writeJSON(rw, 429, map[string]string{"error": "rate limit exceeded", "policy_id": d.PolicyID, "request_id": id})
			return
		}
	}
	g.proxy(rw, r, route)
}
func (g *Runtime) proxy(w http.ResponseWriter, r *http.Request, route config.Route) {
	raw := route.Upstreams[int(g.rr.Add(1)-1)%len(route.Upstreams)]
	target, _ := url.Parse(raw)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: route.UpstreamTimeout.Duration}
	original := proxy.Director
	proxy.Director = func(req *http.Request) {
		original(req)
		if route.StripPrefix {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, route.PathPrefix)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
		}
		for k, v := range route.AddHeaders {
			req.Header.Set(k, v)
		}
		for _, k := range route.RemoveHeaders {
			req.Header.Del(k)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		g.logger.Error("upstream error", "route", route.ID, "error", err)
		writeJSON(w, 502, map[string]string{"error": "upstream unavailable"})
	}
	proxy.ServeHTTP(w, r)
}
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	if e == nil {
		return h
	}
	return r.RemoteAddr
}
func limitKey(r *http.Request, l *config.Limit) string {
	if strings.HasPrefix(l.Key, "header:") {
		return r.Header.Get(strings.TrimPrefix(l.Key, "header:"))
	}
	return clientIP(r)
}
func requestID() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(s int) { r.status = s; r.ResponseWriter.WriteHeader(s) }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func clone(c *config.Config) *config.Config {
	b, _ := json.Marshal(c)
	var out config.Config
	_ = json.Unmarshal(b, &out)
	return &out
}

type Metrics struct {
	mu       sync.Mutex
	requests map[string]uint64
	shadow   map[string]uint64
}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[string]uint64{}, shadow: map[string]uint64{}}
}
func (m *Metrics) Observe(route, decision string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[route+"|"+decision+"|"+strconv.Itoa(status)]++
}
func (m *Metrics) Shadow(policy string) { m.mu.Lock(); defer m.mu.Unlock(); m.shadow[policy]++ }
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte("# HELP gateforge_requests_total Processed requests.\n# TYPE gateforge_requests_total counter\n"))
	for k, v := range m.requests {
		p := strings.Split(k, "|")
		_, _ = w.Write([]byte("gateforge_requests_total{route=\"" + p[0] + "\",decision=\"" + p[1] + "\",status=\"" + p[2] + "\"} " + strconv.FormatUint(v, 10) + "\n"))
	}
	for k, v := range m.shadow {
		_, _ = w.Write([]byte("gateforge_shadow_matches_total{policy=\"" + k + "\"} " + strconv.FormatUint(v, 10) + "\n"))
	}
}
