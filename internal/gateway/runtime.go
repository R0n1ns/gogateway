// Package gateway реализует data plane GateForge.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/policy"
	"github.com/R0n1ns/gogateway/internal/ratelimit"
	"github.com/R0n1ns/gogateway/internal/router"
	"github.com/R0n1ns/gogateway/internal/upstream"
)

type state struct {
	cfg         *config.Config
	router      *router.Router
	engine      *policy.Engine
	pools       map[string]*upstream.Pool
	concurrency map[string]chan struct{}
	trusted     *TrustedProxies
}

func (s *state) close() {
	for _, pool := range s.pools {
		pool.Close()
	}
}

// Runtime атомарно переключает immutable snapshots и обслуживает public traffic.
type Runtime struct {
	current       atomic.Pointer[state]
	limiter       *ratelimit.Limiter
	logger        *slog.Logger
	metrics       *Metrics
	tracer        Tracer
	janitorCancel context.CancelFunc
}

func New(cfg *config.Config, logger *slog.Logger) (*Runtime, error) {
	return NewWithTracer(cfg, logger, NewW3CTracer())
}
func NewWithTracer(cfg *config.Config, logger *slog.Logger, tracer Tracer) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if tracer == nil {
		tracer = NoopTracer{}
	}
	runtime := &Runtime{limiter: ratelimit.New(), logger: logger, metrics: NewMetrics(), tracer: tracer}
	ctx, cancel := context.WithCancel(context.Background())
	runtime.janitorCancel = cancel
	runtime.limiter.StartJanitor(ctx, time.Minute, 10*time.Minute)
	if err := runtime.Update(cfg); err != nil {
		cancel()
		return nil, err
	}
	return runtime, nil
}

func (g *Runtime) Close() {
	if g.janitorCancel != nil {
		g.janitorCancel()
	}
	if current := g.current.Load(); current != nil {
		current.close()
	}
}
func (g *Runtime) Config() *config.Config { return g.current.Load().cfg.DeepCopy() }
func (g *Runtime) Metrics() http.Handler  { return g.metrics }
func (g *Runtime) UpstreamStatus() map[string][]upstream.Status {
	current := g.current.Load()
	out := make(map[string][]upstream.Status, len(current.pools))
	for id, pool := range current.pools {
		out[id] = pool.Status()
	}
	return out
}

// Ready возвращает true, если каждый route имеет хотя бы один healthy upstream.
func (g *Runtime) Ready() bool {
	for _, statuses := range g.UpstreamStatus() {
		healthy := false
		for _, status := range statuses {
			if status.Healthy && !status.CircuitOpen {
				healthy = true
				break
			}
		}
		if !healthy {
			return false
		}
	}
	return true
}

func (g *Runtime) Simulate(r *http.Request, routeID, sourceIP string) policy.Decision {
	return g.current.Load().engine.Evaluate(r, routeID, sourceIP)
}

// Update сначала полностью компилирует новый snapshot и только затем публикует его.
func (g *Runtime) Update(input *config.Config) error {
	prepared, err := Prepare(input)
	if err != nil {
		return err
	}
	g.Publish(prepared, input.Revision)
	return nil
}

// Prepared owns a fully validated snapshot until publication or Close.
// It must not be shared or reused after publication.
type Prepared struct{ next *state }

func (p *Prepared) Close() {
	if p.next != nil {
		p.next.close()
		p.next = nil
	}
}

// Prepare completes all fallible work before a repository commit.
func Prepare(input *config.Config) (*Prepared, error) {
	cfg := input.DeepCopy()
	cfg.Defaults()
	cfg.ExpandEnvironment()
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	engine, err := policy.Compile(cfg.Policies)
	if err != nil {
		return nil, err
	}
	next := &state{cfg: cfg, router: router.New(cfg.Routes), engine: engine, pools: map[string]*upstream.Pool{}, concurrency: map[string]chan struct{}{}}
	next.trusted, err = NewTrustedProxies(cfg.Runtime.TrustedProxies)
	if err != nil {
		return nil, err
	}
	for _, route := range cfg.Routes {
		pool, poolErr := upstream.New(route)
		if poolErr != nil {
			next.close()
			return nil, poolErr
		}
		next.pools[route.ID] = pool
		if route.MaxConcurrent > 0 {
			next.concurrency[route.ID] = make(chan struct{}, route.MaxConcurrent)
		}
	}
	return &Prepared{next: next}, nil
}

// Publish cannot fail. The control service serializes commit and publication.
func (g *Runtime) Publish(prepared *Prepared, revision int64) {
	next := prepared.next
	prepared.next = nil
	next.cfg.Revision = revision
	previous := g.current.Swap(next)
	g.limiter.Reset()
	g.metrics.SetConfigRevision(revision)
	if previous != nil {
		previous.close()
	}
}

func (g *Runtime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r, span := g.tracer.Start(r)
	var spanErr error
	defer func() { span.End(spanErr) }()
	requestID := requestID()
	w.Header().Set("X-Request-ID", requestID)
	if span.TraceID() != "" {
		w.Header().Set("Traceparent", span.TraceParent())
	}
	recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
	routeID, decisionName := "", "allow"
	g.metrics.InFlight(1)
	defer func() {
		g.metrics.InFlight(-1)
		g.metrics.ObserveRequest(routeID, decisionName, recorder.status, time.Since(started))
		cfg := g.current.Load().cfg
		if cfg.AccessLog.Enabled {
			g.logger.Info("access", "request_id", requestID, "trace_id", span.TraceID(), "method", r.Method, "path", r.URL.Path, "route", routeID, "decision", decisionName, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds(), "source_ip", ClientIP(r, g.current.Load().trusted))
		}
	}()
	snapshot := g.current.Load()
	route, ok := snapshot.router.Match(r)
	if !ok {
		decisionName = "route_not_found"
		WriteError(recorder, NewError("route_not_found", http.StatusNotFound, "маршрут не найден", requestID))
		return
	}
	routeID = route.ID
	if route.MaxBodyBytes > 0 {
		if r.ContentLength > route.MaxBodyBytes {
			decisionName = "body_too_large"
			WriteError(recorder, NewError("body_too_large", http.StatusRequestEntityTooLarge, "тело запроса превышает лимит", requestID))
			return
		}
		r.Body = http.MaxBytesReader(recorder, r.Body, route.MaxBodyBytes)
	}
	clientIP := ClientIP(r, snapshot.trusted)
	if !snapshot.trusted.Contains(net.ParseIP(remoteIP(r.RemoteAddr))) {
		r.Header.Del("X-Forwarded-For")
	}
	decision := snapshot.engine.Evaluate(r, route.ID, clientIP)
	for _, id := range decision.Shadow {
		g.logger.Warn("shadow policy matched", "request_id", requestID, "policy_id", id)
		g.metrics.ObservePolicy(id, "shadow")
	}
	if !decision.Allowed {
		decisionName = decision.Action
		if decision.Status == http.StatusUnauthorized {
			recorder.Header().Set("WWW-Authenticate", "Bearer")
		}
		WriteError(recorder, NewPolicyError(decision, requestID))
		return
	}
	if decision.Redirect != nil {
		decisionName = "redirect"
		http.Redirect(recorder, r, decision.Redirect.URL, decision.Redirect.Status)
		return
	}
	applyRewrite(r, decision.Rewrite)
	if decision.Limit != nil {
		key := rateKey(r, decision.Limit, clientIP, decision.Subject)
		result := g.limiter.Acquire(decision.PolicyID+":"+key, *decision.Limit)
		setRateHeaders(recorder.Header(), result)
		if !result.Allowed {
			decisionName = "rate_limit"
			WriteError(recorder, NewError("rate_limit_exceeded", http.StatusTooManyRequests, "превышен лимит запросов", requestID))
			return
		}
		defer result.Release()
	}
	if semaphore := snapshot.concurrency[route.ID]; semaphore != nil {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		default:
			decisionName = "route_concurrency_limit"
			WriteError(recorder, NewError("route_concurrency_limit", http.StatusServiceUnavailable, "маршрут перегружен", requestID))
			return
		}
	}
	if route.MaxBodyBytes > 0 && r.Body != nil {
		body, readErr := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if readErr != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(readErr, &tooLarge) {
				decisionName = "body_too_large"
				WriteError(recorder, NewError("body_too_large", http.StatusRequestEntityTooLarge, "тело запроса превышает лимит", requestID))
			} else {
				WriteError(recorder, NewError("invalid_body", http.StatusBadRequest, "не удалось прочитать тело", requestID))
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.TransferEncoding = nil
	}
	status, backendID, err := g.forward(recorder, r, route, snapshot.pools[route.ID], clientIP)
	if err != nil {
		decisionName = "upstream_error"
		spanErr = err
		code, httpStatus := "upstream_unavailable", http.StatusBadGateway
		if errorsIsTimeout(err) {
			code, httpStatus = "upstream_timeout", http.StatusGatewayTimeout
		}
		WriteError(recorder, NewError(code, httpStatus, "upstream недоступен", requestID))
		return
	}
	g.metrics.ObserveUpstream(route.ID, backendID, status, time.Since(started))
}

func (g *Runtime) forward(w http.ResponseWriter, in *http.Request, route config.Route, pool *upstream.Pool, clientIP string) (int, string, error) {
	attempts := route.Retry.Attempts
	if attempts < 1 {
		attempts = 1
	}
	if !slices.Contains(route.Retry.Methods, in.Method) && attempts > 1 {
		attempts = 1
	}
	var body []byte
	var err error
	if attempts > 1 && in.Body != nil {
		body, err = io.ReadAll(in.Body)
		if err != nil {
			return 0, "", err
		}
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(route.Retry.Backoff.Duration):
			case <-in.Context().Done():
				return 0, "", in.Context().Err()
			}
		}
		selection, selectErr := pool.Select(in)
		if selectErr != nil {
			return 0, "", selectErr
		}
		backendID := selection.Backend.ID()
		outbound := cloneRequest(in, selection.Backend.Target(), route, clientIP, body, attempts > 1)
		response, roundErr := pool.Transport().RoundTrip(outbound)
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		pool.ObserveResult(selection.Backend, status, roundErr)
		selection.Release()
		if roundErr != nil {
			lastErr = roundErr
			if attempt+1 < attempts {
				continue
			}
			return 0, backendID, roundErr
		}
		retryable := slices.Contains(route.Retry.Statuses, response.StatusCode) && attempt+1 < attempts
		if retryable {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			lastErr = &statusError{status: response.StatusCode}
			continue
		}
		copyHeader(w.Header(), response.Header)
		for key, value := range route.AddResponseHeaders {
			w.Header().Set(key, value)
		}
		w.WriteHeader(response.StatusCode)
		_, copyErr := io.Copy(w, response.Body)
		_ = response.Body.Close()
		return response.StatusCode, backendID, copyErr
	}
	return 0, "", lastErr
}

func applyRewrite(r *http.Request, rewrite *config.Rewrite) {
	if rewrite == nil {
		return
	}
	if rewrite.PathPrefix != "" {
		r.URL.Path = rewrite.PathPrefix + r.URL.Path
	}
	for key, value := range rewrite.SetHeaders {
		r.Header.Set(key, value)
	}
}
func rateKey(r *http.Request, limit *config.Limit, ip, subject string) string {
	switch {
	case limit.Key == "jwt_subject":
		return subject
	case strings.HasPrefix(limit.Key, "header:"):
		return r.Header.Get(strings.TrimPrefix(limit.Key, "header:"))
	default:
		return ip
	}
}
func setRateHeaders(header http.Header, result ratelimit.Result) {
	header.Set("RateLimit-Limit", strconv.Itoa(result.Limit))
	header.Set("RateLimit-Remaining", strconv.Itoa(result.Remaining))
	if !result.Allowed && !result.ResetAt.IsZero() {
		header.Set("RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
		header.Set("Retry-After", strconv.Itoa(max(1, int(time.Until(result.ResetAt).Seconds()))))
	}
}
func requestID() string {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(data)
}

type statusError struct{ status int }

func (e *statusError) Error() string { return "upstream status " + strconv.Itoa(e.status) }
