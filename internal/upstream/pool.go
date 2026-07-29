// Package upstream управляет backend-пулами, балансировкой и состоянием здоровья.
package upstream

import (
	"context"
	"errors"
	"hash/fnv"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

var ErrNoHealthyUpstream = errors.New("нет доступных upstream")

// Backend содержит неизменяемую конфигурацию и атомарное runtime-состояние.
type Backend struct {
	id                 string
	target             *url.URL
	weight             int
	healthy            atomic.Bool
	active             atomic.Int64
	mu                 sync.Mutex
	consecutiveSuccess int
	consecutiveFailure int
	openedAt           time.Time
	halfOpen           int
}

func (b *Backend) ID() string       { return b.id }
func (b *Backend) Target() *url.URL { copy := *b.target; return &copy }
func (b *Backend) Active() int64    { return b.active.Load() }
func (b *Backend) Healthy() bool    { return b.healthy.Load() }

// Selection резервирует active slot. Release должна вызываться ровно один раз;
// повторный вызов безопасен.
type Selection struct {
	Backend *Backend
	once    sync.Once
}

func (s *Selection) Release() {
	if s == nil || s.Backend == nil {
		return
	}
	s.once.Do(func() { s.Backend.active.Add(-1) })
}

// Pool живёт столько же, сколько compiled route snapshot.
type Pool struct {
	route     config.Route
	backends  []*Backend
	transport *http.Transport
	sequence  atomic.Uint64
	randomMu  sync.Mutex
	random    *rand.Rand
	cancel    context.CancelFunc
}

func New(route config.Route) (*Pool, error) {
	pool := &Pool{route: route, random: rand.New(rand.NewSource(time.Now().UnixNano()))}
	pool.transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: route.UpstreamTimeout.Duration,
	}
	for _, item := range route.UpstreamPool {
		target, err := url.Parse(item.URL)
		if err != nil {
			return nil, err
		}
		backend := &Backend{id: item.ID, target: target, weight: item.Weight}
		backend.healthy.Store(true)
		pool.backends = append(pool.backends, backend)
	}
	if len(pool.backends) == 0 {
		return nil, ErrNoHealthyUpstream
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool.cancel = cancel
	if route.HealthCheck.Enabled {
		pool.startHealthChecks(ctx)
	}
	return pool, nil
}

func (p *Pool) Transport() http.RoundTripper { return p.transport }
func (p *Pool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.transport.CloseIdleConnections()
}

// Select выбирает только healthy backend с закрытым circuit breaker.
func (p *Pool) Select(r *http.Request) (*Selection, error) {
	candidates := make([]*Backend, 0, len(p.backends))
	now := time.Now()
	for _, backend := range p.backends {
		if backend.healthy.Load() && p.circuitAllows(backend, now) {
			candidates = append(candidates, backend)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrNoHealthyUpstream
	}
	var selected *Backend
	switch p.route.Balance.Strategy {
	case "weighted_round_robin":
		selected = p.weighted(candidates)
	case "least_connections":
		selected = candidates[0]
		for _, item := range candidates[1:] {
			if item.active.Load() < selected.active.Load() {
				selected = item
			}
		}
	case "random":
		p.randomMu.Lock()
		selected = candidates[p.random.Intn(len(candidates))]
		p.randomMu.Unlock()
	case "consistent_hash":
		selected = candidates[int(hashValue(hashKey(r, p.route.Balance.HashKey))%uint64(len(candidates)))]
	default:
		selected = candidates[int((p.sequence.Add(1)-1)%uint64(len(candidates)))]
	}
	selected.active.Add(1)
	return &Selection{Backend: selected}, nil
}

func (p *Pool) weighted(items []*Backend) *Backend {
	total := 0
	for _, item := range items {
		total += item.weight
	}
	point := int((p.sequence.Add(1) - 1) % uint64(total))
	for _, item := range items {
		if point < item.weight {
			return item
		}
		point -= item.weight
	}
	return items[0]
}
func hashValue(value string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return hash.Sum64()
}
func hashKey(r *http.Request, key string) string {
	switch {
	case key == "ip":
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			return host
		}
		return r.RemoteAddr
	case key == "path":
		return r.URL.Path
	case strings.HasPrefix(key, "header:"):
		return r.Header.Get(strings.TrimPrefix(key, "header:"))
	case strings.HasPrefix(key, "cookie:"):
		cookie, err := r.Cookie(strings.TrimPrefix(key, "cookie:"))
		if err == nil {
			return cookie.Value
		}
	}
	return r.RemoteAddr
}

// ObserveResult обновляет passive health и circuit breaker.
func (p *Pool) ObserveResult(backend *Backend, status int, err error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	failed := err != nil || status >= 500
	if failed {
		backend.consecutiveFailure++
		backend.consecutiveSuccess = 0
		if threshold := p.route.CircuitBreaker.FailureThreshold; threshold > 0 && backend.consecutiveFailure >= threshold {
			backend.openedAt = time.Now()
			backend.halfOpen = 0
		}
	} else {
		backend.consecutiveSuccess++
		backend.consecutiveFailure = 0
		backend.openedAt = time.Time{}
		backend.halfOpen = 0
	}
}
func (p *Pool) circuitAllows(backend *Backend, now time.Time) bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.openedAt.IsZero() {
		return true
	}
	if now.Sub(backend.openedAt) < p.route.CircuitBreaker.OpenTimeout.Duration {
		return false
	}
	max := p.route.CircuitBreaker.HalfOpenRequests
	if max < 1 {
		max = 1
	}
	if backend.halfOpen >= max {
		return false
	}
	backend.halfOpen++
	return true
}

type Status struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Healthy     bool   `json:"healthy"`
	Active      int64  `json:"active"`
	CircuitOpen bool   `json:"circuit_open"`
	Failures    int    `json:"consecutive_failures"`
}

func (p *Pool) Status() []Status {
	now := time.Now()
	out := make([]Status, 0, len(p.backends))
	for _, b := range p.backends {
		b.mu.Lock()
		safeURL := (&url.URL{Scheme: b.target.Scheme, Host: b.target.Host}).String()
		out = append(out, Status{ID: b.id, URL: safeURL, Healthy: b.healthy.Load(), Active: b.active.Load(), CircuitOpen: !b.openedAt.IsZero() && now.Sub(b.openedAt) < p.route.CircuitBreaker.OpenTimeout.Duration, Failures: b.consecutiveFailure})
		b.mu.Unlock()
	}
	return out
}
