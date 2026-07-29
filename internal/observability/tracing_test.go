package observability

import (
	"context"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestProviderShutdownБезПровайдера(t *testing.T) {
	var nilProvider *Provider
	if err := nilProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() для nil вернул ошибку: %v", err)
	}

	provider := &Provider{}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() для пустого провайдера вернул ошибку: %v", err)
	}
}

func TestSpanВозвращаетКонтекстИИгнорируетОтмену(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := newAdapter(provider.Tracer("test"), propagation.TraceContext{})

	request := httptest.NewRequest("POST", "http://gateway/orders", nil)
	updatedRequest, span := tracer.Start(request)
	if updatedRequest == request {
		t.Fatal("Start() должен вернуть запрос с новым контекстом")
	}
	if span.TraceID() == "" {
		t.Fatal("TraceID() не должен быть пустым")
	}
	if span.TraceParent() == "" {
		t.Fatal("TraceParent() не должен быть пустым для валидного span")
	}

	span.End(context.Canceled)
	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("завершено spans=%d, ожидался 1", len(ended))
	}
	if ended[0].Status().Code != 0 {
		t.Fatalf("отмена контекста не должна отмечать span как ошибочный: %v", ended[0].Status().Code)
	}
}

func TestTraceParentДляНевалидногоКонтекста(t *testing.T) {
	_, rawSpan := noop.NewTracerProvider().Tracer("test").Start(context.Background(), "test")
	span := otelSpan{span: rawSpan}
	if got := span.TraceParent(); got != "" {
		t.Fatalf("TraceParent()=%q, ожидалась пустая строка", got)
	}
	span.End(nil)
}
