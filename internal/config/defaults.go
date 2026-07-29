package config

import (
	"os"
	"slices"
	"strings"
	"time"
)

// Defaults заполняет безопасные значения, не меняя явно заданные настройки.
func (c *Config) Defaults() {
	defaultDuration(&c.Public.ReadTimeout, 10*time.Second)
	defaultDuration(&c.Public.WriteTimeout, 30*time.Second)
	defaultDuration(&c.Public.IdleTimeout, time.Minute)
	defaultDuration(&c.Admin.ReadTimeout, 5*time.Second)
	defaultDuration(&c.Admin.WriteTimeout, 10*time.Second)
	defaultDuration(&c.Admin.IdleTimeout, 30*time.Second)
	defaultDuration(&c.Runtime.ShutdownTimeout, 10*time.Second)
	if c.Runtime.MaxConfigBytes == 0 {
		c.Runtime.MaxConfigBytes = 1 << 20
	}
	if c.AccessLog.Format == "" {
		c.AccessLog.Format = "json"
	}
	if c.Storage.Driver == "" {
		c.Storage.Driver = "memory"
	}
	if c.Storage.MaxOpenConns == 0 {
		c.Storage.MaxOpenConns = 10
	}
	if c.Storage.MaxIdleConns == 0 {
		c.Storage.MaxIdleConns = 5
	}
	if c.Observability.ServiceName == "" {
		c.Observability.ServiceName = "gateforge"
	}
	if c.Observability.SampleRate == 0 {
		c.Observability.SampleRate = 1
	}
	for i := range c.Routes {
		route := &c.Routes[i]
		defaultDuration(&route.UpstreamTimeout, 30*time.Second)
		if route.Balance.Strategy == "" {
			route.Balance.Strategy = "round_robin"
		}
		if route.HealthCheck.Enabled {
			if route.HealthCheck.Path == "" {
				route.HealthCheck.Path = "/healthz"
			}
			defaultDuration(&route.HealthCheck.Interval, 10*time.Second)
			defaultDuration(&route.HealthCheck.Timeout, 2*time.Second)
			if route.HealthCheck.HealthyThreshold == 0 {
				route.HealthCheck.HealthyThreshold = 2
			}
			if route.HealthCheck.UnhealthyThreshold == 0 {
				route.HealthCheck.UnhealthyThreshold = 3
			}
		}
		if route.Retry.Attempts == 0 {
			route.Retry.Attempts = 1
		}
		if route.Retry.Attempts > 1 {
			defaultDuration(&route.Retry.Backoff, 50*time.Millisecond)
			if len(route.Retry.Methods) == 0 {
				route.Retry.Methods = []string{"GET", "HEAD", "OPTIONS"}
			}
			if len(route.Retry.Statuses) == 0 {
				route.Retry.Statuses = []int{502, 503, 504}
			}
		}
		if route.CircuitBreaker.FailureThreshold > 0 {
			defaultDuration(&route.CircuitBreaker.OpenTimeout, 30*time.Second)
			if route.CircuitBreaker.HalfOpenRequests == 0 {
				route.CircuitBreaker.HalfOpenRequests = 1
			}
		}
	}
	for i := range c.Policies {
		policy := &c.Policies[i]
		if policy.Mode == "" {
			policy.Mode = ModeBlock
		}
		if policy.Limit != nil {
			if policy.Limit.Algorithm == "" {
				policy.Limit.Algorithm = "token_bucket"
			}
			defaultDuration(&policy.Limit.Window, time.Minute)
			defaultDuration(&policy.Limit.IdleTTL, 10*time.Minute)
		}
		if policy.Redirect != nil && policy.Redirect.Status == 0 {
			policy.Redirect.Status = 302
		}
		if policy.Auth != nil && policy.Auth.Header == "" {
			policy.Auth.Header = "Authorization"
		}
	}
}

func defaultDuration(target *Duration, value time.Duration) {
	if target.Duration == 0 {
		target.Duration = value
	}
}

// Normalize приводит host, методы, пути и идентификаторы к каноническому виду.
func (c *Config) Normalize() {
	for i := range c.Routes {
		route := &c.Routes[i]
		route.Host = strings.ToLower(route.Host)
		if route.PathPrefix == "" {
			route.PathPrefix = "/"
		}
		if len(route.UpstreamPool) == 0 {
			for j, raw := range route.Upstreams {
				route.UpstreamPool = append(route.UpstreamPool, Upstream{ID: route.ID + "-" + itoa(j+1), URL: raw, Weight: 1})
			}
		}
		for j := range route.UpstreamPool {
			if route.UpstreamPool[j].ID == "" {
				route.UpstreamPool[j].ID = route.ID + "-" + itoa(j+1)
			}
			if route.UpstreamPool[j].Weight == 0 {
				route.UpstreamPool[j].Weight = 1
			}
		}
		for j := range route.Retry.Methods {
			route.Retry.Methods[j] = strings.ToUpper(route.Retry.Methods[j])
		}
	}
	for i := range c.Policies {
		normalizeMatch(&c.Policies[i].Match)
	}
	slices.SortStableFunc(c.Policies, func(a, b Policy) int { return a.Priority - b.Priority })
}

func normalizeMatch(match *Match) {
	for i := range match.Methods {
		match.Methods[i] = strings.ToUpper(match.Methods[i])
	}
	for i := range match.All {
		normalizeMatch(&match.All[i])
	}
	for i := range match.Any {
		normalizeMatch(&match.Any[i])
	}
	if match.Not != nil {
		normalizeMatch(match.Not)
	}
}

// ExpandEnvironment подставляет ${NAME} только в полях, где допустимы секреты
// или адреса. Неизвестная переменная заменяется пустой строкой и затем попадает
// под обычную валидацию.
func (c *Config) ExpandEnvironment() {
	expand := func(value string) string {
		return environmentPattern.ReplaceAllStringFunc(value, func(token string) string {
			groups := environmentPattern.FindStringSubmatch(token)
			return os.Getenv(groups[1])
		})
	}
	c.Admin.APIKey = expand(c.Admin.APIKey)
	c.Storage.DSN = expand(c.Storage.DSN)
	for i := range c.Routes {
		for j := range c.Routes[i].Upstreams {
			c.Routes[i].Upstreams[j] = expand(c.Routes[i].Upstreams[j])
		}
		for j := range c.Routes[i].UpstreamPool {
			c.Routes[i].UpstreamPool[j].URL = expand(c.Routes[i].UpstreamPool[j].URL)
		}
	}
	for i := range c.Policies {
		auth := c.Policies[i].Auth
		if auth != nil {
			auth.Value = expand(auth.Value)
			auth.Username = expand(auth.Username)
			auth.Password = expand(auth.Password)
			auth.JWTSecret = expand(auth.JWTSecret)
		}
	}
}

func itoa(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
