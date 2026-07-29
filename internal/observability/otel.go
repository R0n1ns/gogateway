// Package observability подключает OpenTelemetry к gateway tracing contract.
package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

type Provider struct {
	Tracer   gateway.Tracer
	provider *sdktrace.TracerProvider
}

// NewOTLP создаёт batch exporter. Endpoint должен включать scheme и host.
func NewOTLP(ctx context.Context, cfg config.Observability) (*Provider, error) {
	options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint)}
	if cfg.OTLPInsecure {
		options = append(options, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("создать OTLP exporter: %w", err)
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(cfg.ServiceName)))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))))
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagator)
	return &Provider{Tracer: newAdapter(provider.Tracer("github.com/R0n1ns/gogateway"), propagator), provider: provider}, nil
}
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

type adapter struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

func newAdapter(tracer trace.Tracer, propagator propagation.TextMapPropagator) gateway.Tracer {
	return adapter{tracer: tracer, propagator: propagator}
}
func (a adapter) Start(r *http.Request) (*http.Request, gateway.Span) {
	ctx := a.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := a.tracer.Start(ctx, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", r.Method), attribute.String("url.path", r.URL.Path)))
	a.propagator.Inject(ctx, propagation.HeaderCarrier(r.Header))
	return r.WithContext(ctx), otelSpan{span: span}
}

type otelSpan struct{ span trace.Span }

func (s otelSpan) TraceID() string { return s.span.SpanContext().TraceID().String() }
func (s otelSpan) TraceParent() string {
	spanContext := s.span.SpanContext()
	if !spanContext.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%02x", spanContext.TraceID(), spanContext.SpanID(), byte(spanContext.TraceFlags()))
}
func (s otelSpan) End(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		s.span.RecordError(err)
		s.span.SetStatus(codes.Error, err.Error())
	}
	s.span.End()
}
