package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/ratelimit"
)

func TestPrometheusHistogramContainsRequiredSeries(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveRequest("api", "allow", 200, 25*time.Millisecond)

	response := httptest.NewRecorder()
	metrics.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		"gateforge_request_duration_seconds_bucket{le=\"+Inf\"} 1",
		"gateforge_request_duration_seconds_sum 0.025",
		"gateforge_request_duration_seconds_count 1",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("в метриках отсутствует %q", expected)
		}
	}
}

func TestRetryAfterOnlyForRejectedRequest(t *testing.T) {
	allowed := httptest.NewRecorder()
	setRateHeaders(allowed.Header(), ratelimit.Result{Allowed: true, Limit: 10, Remaining: 9, ResetAt: time.Now().Add(time.Minute)})
	if value := allowed.Header().Get("Retry-After"); value != "" {
		t.Fatalf("разрешённый запрос получил Retry-After=%q", value)
	}

	rejected := httptest.NewRecorder()
	setRateHeaders(rejected.Header(), ratelimit.Result{Allowed: false, Limit: 10, ResetAt: time.Now().Add(time.Minute)})
	if value := rejected.Header().Get("Retry-After"); value == "" {
		t.Fatal("отклонённый запрос не получил Retry-After")
	}
}
