package observability

import (
	"errors"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAdapterContinuesTraceAndRecordsError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	adapter := newAdapter(provider.Tracer("test"), propagation.TraceContext{})
	request := httptest.NewRequest("GET", "http://gateway/api", nil)
	request.Header.Set("Traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	_, span := adapter.Start(request)
	span.End(errors.New("failed"))
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("spans=%d", len(ended))
	}
	if got := ended[0].SpanContext().TraceID().String(); got != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("traceID=%s", got)
	}
	if ended[0].Status().Code == 0 {
		t.Fatal("error status не записан")
	}
}
