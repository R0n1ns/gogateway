package ratelimit

import (
	"testing"
	"time"
)

func TestLimitAndWindow(t *testing.T) {
	limiter := New()
	now := time.Unix(1, 0)
	limiter.now = func() time.Time { return now }
	results := []bool{
		limiter.Allow("a", 2, 1, time.Minute),
		limiter.Allow("a", 2, 1, time.Minute),
		limiter.Allow("a", 2, 1, time.Minute),
		limiter.Allow("a", 2, 1, time.Minute),
	}
	if !results[0] || !results[1] || !results[2] || results[3] {
		t.Fatalf("неверная последовательность решений: %v", results)
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("a", 2, 1, time.Minute) {
		t.Fatal("окно не сбросилось")
	}
}
