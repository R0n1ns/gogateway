package ratelimit

import (
	"testing"
	"time"
)

func TestLimitAndWindow(t *testing.T) {
	l := New()
	now := time.Unix(1, 0)
	l.now = func() time.Time { return now }
	if !l.Allow("a", 2, 1, time.Minute) || !l.Allow("a", 2, 1, time.Minute) || !l.Allow("a", 2, 1, time.Minute) || l.Allow("a", 2, 1, time.Minute) {
		t.Fatal("неверный лимит")
	}
	now = now.Add(time.Minute)
	if !l.Allow("a", 2, 1, time.Minute) {
		t.Fatal("окно не сбросилось")
	}
}
