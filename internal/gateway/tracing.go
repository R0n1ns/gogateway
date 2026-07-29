package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

type traceKey struct{}

// Tracer позволяет подключить OpenTelemetry adapter, не связывая data plane с SDK.
type Tracer interface {
	Start(*http.Request) (*http.Request, Span)
}
type Span interface {
	TraceID() string
	TraceParent() string
	End(error)
}
type NoopTracer struct{}

func (NoopTracer) Start(r *http.Request) (*http.Request, Span) { return r, noopSpan{} }

type noopSpan struct{}

func (noopSpan) TraceID() string     { return "" }
func (noopSpan) TraceParent() string { return "" }
func (noopSpan) End(error)           {}

type W3CTracer struct{}
type w3cSpan struct{ traceID, spanID string }

var traceParentPattern = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$`)

func NewW3CTracer() Tracer { return W3CTracer{} }
func (W3CTracer) Start(r *http.Request) (*http.Request, Span) {
	traceID := ""
	if match := traceParentPattern.FindStringSubmatch(r.Header.Get("Traceparent")); len(match) > 0 {
		traceID = match[1]
	}
	if traceID == "" {
		traceID = randomHex(16)
	}
	span := w3cSpan{traceID: traceID, spanID: randomHex(8)}
	parent := span.TraceParent()
	r.Header.Set("Traceparent", parent)
	ctx := context.WithValue(r.Context(), traceKey{}, traceID)
	return r.WithContext(ctx), span
}
func (s w3cSpan) TraceID() string     { return s.traceID }
func (s w3cSpan) TraceParent() string { return "00-" + s.traceID + "-" + s.spanID + "-01" }
func (s w3cSpan) End(error)           {}
func randomHex(size int) string {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return hex.EncodeToString(data)
}
