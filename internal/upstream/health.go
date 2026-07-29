package upstream

import (
	"context"
	"net/http"
	"time"
)

func (p *Pool) startHealthChecks(ctx context.Context) {
	for _, backend := range p.backends {
		go p.healthLoop(ctx, backend)
	}
}

func (p *Pool) healthLoop(ctx context.Context, backend *Backend) {
	ticker := time.NewTicker(p.route.HealthCheck.Interval.Duration)
	defer ticker.Stop()
	p.check(ctx, backend)
	for {
		select {
		case <-ticker.C:
			p.check(ctx, backend)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pool) check(parent context.Context, backend *Backend) {
	ctx, cancel := context.WithTimeout(parent, p.route.HealthCheck.Timeout.Duration)
	defer cancel()
	target := *backend.target
	target.Path = p.route.HealthCheck.Path
	target.RawQuery = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	healthy := false
	if err == nil {
		response, roundErr := p.transport.RoundTrip(request)
		if roundErr == nil {
			healthy = response.StatusCode >= 200 && response.StatusCode < 400
			_ = response.Body.Close()
		}
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if healthy {
		backend.consecutiveSuccess++
		backend.consecutiveFailure = 0
		if backend.consecutiveSuccess >= p.route.HealthCheck.HealthyThreshold {
			backend.healthy.Store(true)
		}
	} else {
		backend.consecutiveFailure++
		backend.consecutiveSuccess = 0
		if backend.consecutiveFailure >= p.route.HealthCheck.UnhealthyThreshold {
			backend.healthy.Store(false)
		}
	}
}
