// Package ratelimit реализует потокобезопасный fixed-window limiter.
package ratelimit

import (
	"sync"
	"time"
)

type entry struct {
	start time.Time
	used  int
}
type Limiter struct {
	mu    sync.Mutex
	items map[string]entry
	now   func() time.Time
}

func New() *Limiter { return &Limiter{items: map[string]entry{}, now: time.Now} }
func (l *Limiter) Allow(key string, requests, burst int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.now()
	v := l.items[key]
	if v.start.IsZero() || n.Sub(v.start) >= window {
		v = entry{start: n}
	}
	max := requests + burst
	if max < 1 {
		max = 1
	}
	if v.used >= max {
		l.items[key] = v
		return false
	}
	v.used++
	l.items[key] = v
	return true
}
func (l *Limiter) Reset() { l.mu.Lock(); defer l.mu.Unlock(); l.items = map[string]entry{} }
