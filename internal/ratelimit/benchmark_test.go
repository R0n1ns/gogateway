package ratelimit

import (
	"strconv"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func BenchmarkAcquireParallel(b *testing.B) {
	limiter := New()
	spec := config.Limit{Requests: 1_000_000, Window: config.Duration{Duration: time.Minute}, Burst: 1_000_000}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		id := 0
		for pb.Next() {
			result := limiter.Acquire(strconv.Itoa(id%1000), spec)
			result.Release()
			id++
		}
	})
}
