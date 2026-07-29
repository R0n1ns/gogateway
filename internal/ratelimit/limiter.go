// Package ratelimit реализует token bucket и ограничение конкурентных запросов.
package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

type bucket struct {
	tokens    float64
	updatedAt time.Time
	lastSeen  time.Time
	inFlight  int
	idleTTL   time.Duration
}

// Result описывает решение limiter и содержит Release для concurrency slot.
type Result struct {
	Allowed   bool
	Limit     int
	Remaining int
	ResetAt   time.Time
	Release   func()
}

// Limiter безопасен для конкурентного использования всеми route/policy.
type Limiter struct {
	mu    sync.Mutex
	items map[string]*bucket
	now   func() time.Time
}

func New() *Limiter { return &Limiter{items: make(map[string]*bucket), now: time.Now} }

// Allow — совместимый helper старого MVP.
func (l *Limiter) Allow(key string, requests, burst int, window time.Duration) bool {
	return l.Acquire(key, config.Limit{Requests: requests, Burst: burst, Window: config.Duration{Duration: window}}).Allowed
}

// Acquire пополняет bucket пропорционально прошедшему времени. Максимальная
// ёмкость равна requests+burst; MaxConcurrent резервирует отдельный slot.
func (l *Limiter) Acquire(key string, limit config.Limit) Result {
	l.mu.Lock()
	now := l.now()
	capacity := limit.Requests + limit.Burst
	if capacity < 1 {
		capacity = 1
	}
	item := l.items[key]
	if item == nil {
		item = &bucket{tokens: float64(capacity), updatedAt: now, lastSeen: now, idleTTL: limit.IdleTTL.Duration}
		l.items[key] = item
	}
	elapsed := now.Sub(item.updatedAt).Seconds()
	if elapsed > 0 && limit.Window.Duration > 0 {
		rate := float64(limit.Requests) / limit.Window.Seconds()
		item.tokens = math.Min(float64(capacity), item.tokens+elapsed*rate)
		item.updatedAt = now
	}
	item.lastSeen = now
	if limit.IdleTTL.Duration > 0 {
		item.idleTTL = limit.IdleTTL.Duration
	}
	reset := now
	if item.tokens < 1 && limit.Requests > 0 {
		reset = now.Add(time.Duration((1 - item.tokens) / (float64(limit.Requests) / limit.Window.Seconds()) * float64(time.Second)))
	}
	if item.tokens < 1 || (limit.MaxConcurrent > 0 && item.inFlight >= limit.MaxConcurrent) {
		remaining := int(math.Floor(item.tokens))
		if remaining < 0 {
			remaining = 0
		}
		l.mu.Unlock()
		return Result{Allowed: false, Limit: capacity, Remaining: remaining, ResetAt: reset, Release: func() {}}
	}
	item.tokens--
	if limit.MaxConcurrent > 0 {
		item.inFlight++
	}
	remaining := int(math.Floor(item.tokens))
	l.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			if limit.MaxConcurrent <= 0 {
				return
			}
			l.mu.Lock()
			if current := l.items[key]; current != nil && current.inFlight > 0 {
				current.inFlight--
			}
			l.mu.Unlock()
		})
	}
	return Result{Allowed: true, Limit: capacity, Remaining: remaining, ResetAt: reset, Release: release}
}

// Cleanup удаляет неактивные buckets и предотвращает бесконечный рост памяти.
func (l *Limiter) Cleanup(now time.Time, fallbackTTL time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	removed := 0
	for key, item := range l.items {
		ttl := item.idleTTL
		if ttl <= 0 {
			ttl = fallbackTTL
		}
		if item.inFlight == 0 && ttl > 0 && now.Sub(item.lastSeen) >= ttl {
			delete(l.items, key)
			removed++
		}
	}
	return removed
}

// StartJanitor запускает очистку до отмены context.
func (l *Limiter) StartJanitor(ctx context.Context, interval, fallbackTTL time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				l.Cleanup(now, fallbackTTL)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (l *Limiter) Size() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.items) }
func (l *Limiter) Reset()    { l.mu.Lock(); defer l.mu.Unlock(); l.items = make(map[string]*bucket) }
