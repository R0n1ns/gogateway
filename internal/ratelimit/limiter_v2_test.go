package ratelimit

import (
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestTokenBucketRefillsGradually(t *testing.T) {
	limiter := New()
	now := time.Unix(100, 0)
	limiter.now = func() time.Time { return now }
	spec := config.Limit{Requests: 2, Burst: 0, Window: config.Duration{Duration: time.Second}}
	first := limiter.Acquire("client", spec).Allowed
	second := limiter.Acquire("client", spec).Allowed
	third := limiter.Acquire("client", spec).Allowed
	if !first || !second || third {
		t.Fatalf("неверная начальная ёмкость: %v %v %v", first, second, third)
	}
	now = now.Add(500 * time.Millisecond)
	if !limiter.Acquire("client", spec).Allowed {
		t.Fatal("token не восстановился постепенно")
	}
}

func TestConcurrencyReleaseIsIdempotent(t *testing.T) {
	limiter := New()
	spec := config.Limit{Requests: 10, Window: config.Duration{Duration: time.Minute}, MaxConcurrent: 1}
	first := limiter.Acquire("client", spec)
	if !first.Allowed {
		t.Fatal("первый запрос отклонён")
	}
	if limiter.Acquire("client", spec).Allowed {
		t.Fatal("concurrency limit не применён")
	}
	first.Release()
	first.Release()
	if !limiter.Acquire("client", spec).Allowed {
		t.Fatal("slot не освобождён")
	}
}

func TestCleanupRemovesOnlyIdleBuckets(t *testing.T) {
	limiter := New()
	now := time.Unix(100, 0)
	limiter.now = func() time.Time { return now }
	result := limiter.Acquire("idle", config.Limit{Requests: 1, Window: config.Duration{Duration: time.Minute}, IdleTTL: config.Duration{Duration: time.Minute}})
	result.Release()
	if removed := limiter.Cleanup(now.Add(time.Minute), 10*time.Minute); removed != 1 || limiter.Size() != 0 {
		t.Fatalf("removed=%d size=%d", removed, limiter.Size())
	}
}
