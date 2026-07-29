package gateway

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type requestMetric struct{ Route, Decision, Status string }
type upstreamMetric struct{ Route, Upstream, Status string }

// Metrics — dependency-free Prometheus exposition с bounded labels.
type Metrics struct {
	mu              sync.RWMutex
	requests        map[requestMetric]uint64
	policies        map[string]uint64
	upstream        map[upstreamMetric]uint64
	latency         [7]uint64
	upstreamLatency [7]uint64
	inFlight        atomic.Int64
	revision        atomic.Int64
}

var histogramBounds = []float64{.005, .01, .025, .05, .1, .5, 1}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[requestMetric]uint64{}, policies: map[string]uint64{}, upstream: map[upstreamMetric]uint64{}}
}
func (m *Metrics) InFlight(delta int64)          { m.inFlight.Add(delta) }
func (m *Metrics) SetConfigRevision(value int64) { m.revision.Store(value) }
func (m *Metrics) ObserveRequest(route, decision string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[requestMetric{safeLabel(route), safeLabel(decision), strconv.Itoa(status)}]++
	observeHistogram(&m.latency, duration)
}
func (m *Metrics) ObservePolicy(id, mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[safeLabel(id)+"|"+safeLabel(mode)]++
}
func (m *Metrics) ObserveUpstream(route, id string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upstream[upstreamMetric{safeLabel(route), safeLabel(id), strconv.Itoa(status)}]++
	observeHistogram(&m.upstreamLatency, duration)
}
func (m *Metrics) Shadow(id string) { m.ObservePolicy(id, "shadow") }
func (m *Metrics) Observe(route, decision string, status int) {
	m.ObserveRequest(route, decision, status, 0)
}
func observeHistogram(target *[7]uint64, duration time.Duration) {
	seconds := duration.Seconds()
	for i, bound := range histogramBounds {
		if seconds <= bound {
			target[i]++
		}
	}
}
func safeLabel(value string) string {
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
func quote(value string) string { return strconv.Quote(value) }

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# TYPE gateforge_requests_in_flight gauge\ngateforge_requests_in_flight %d\n# TYPE gateforge_config_revision gauge\ngateforge_config_revision %d\n", m.inFlight.Load(), m.revision.Load())
	requestKeys := make([]requestMetric, 0, len(m.requests))
	for key := range m.requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool { return fmt.Sprint(requestKeys[i]) < fmt.Sprint(requestKeys[j]) })
	_, _ = fmt.Fprintln(w, "# TYPE gateforge_requests_total counter")
	for _, key := range requestKeys {
		_, _ = fmt.Fprintf(w, "gateforge_requests_total{route=%s,decision=%s,status=%s} %d\n", quote(key.Route), quote(key.Decision), quote(key.Status), m.requests[key])
	}
	_, _ = fmt.Fprintln(w, "# TYPE gateforge_policy_matches_total counter")
	policyKeys := make([]string, 0, len(m.policies))
	for key := range m.policies {
		policyKeys = append(policyKeys, key)
	}
	sort.Strings(policyKeys)
	for _, key := range policyKeys {
		parts := splitMetricKey(key)
		_, _ = fmt.Fprintf(w, "gateforge_policy_matches_total{policy=%s,mode=%s} %d\n", quote(parts[0]), quote(parts[1]), m.policies[key])
	}
	_, _ = fmt.Fprintln(w, "# TYPE gateforge_upstream_requests_total counter")
	upstreamKeys := make([]upstreamMetric, 0, len(m.upstream))
	for key := range m.upstream {
		upstreamKeys = append(upstreamKeys, key)
	}
	sort.Slice(upstreamKeys, func(i, j int) bool { return fmt.Sprint(upstreamKeys[i]) < fmt.Sprint(upstreamKeys[j]) })
	for _, key := range upstreamKeys {
		_, _ = fmt.Fprintf(w, "gateforge_upstream_requests_total{route=%s,upstream=%s,status=%s} %d\n", quote(key.Route), quote(key.Upstream), quote(key.Status), m.upstream[key])
	}
	writeHistogram(w, "gateforge_request_duration_seconds", m.latency)
	writeHistogram(w, "gateforge_upstream_duration_seconds", m.upstreamLatency)
}
func writeHistogram(w http.ResponseWriter, name string, values [7]uint64) {
	_, _ = fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	for i, bound := range histogramBounds {
		_, _ = fmt.Fprintf(w, "%s_bucket{le=%s} %d\n", name, quote(strconv.FormatFloat(bound, 'f', -1, 64)), values[i])
	}
}
func splitMetricKey(value string) [2]string {
	for i, char := range value {
		if char == '|' {
			return [2]string{value[:i], value[i+1:]}
		}
	}
	return [2]string{value, ""}
}
