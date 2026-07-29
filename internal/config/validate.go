package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Validate собирает все ошибки документа, чтобы оператор исправил их за один проход.
func (c *Config) Validate() error {
	var errs []error
	if c.Version < 1 {
		errs = append(errs, errors.New("version должна быть не меньше 1"))
	}
	if c.Public.Address == "" {
		errs = append(errs, errors.New("public.address обязателен"))
	}
	if c.Admin.Address == "" {
		errs = append(errs, errors.New("admin.address обязателен"))
	}
	if c.Storage.Driver != "memory" && c.Storage.Driver != "postgres" {
		errs = append(errs, errors.New("storage.driver должен быть memory или postgres"))
	}
	if c.Storage.Driver == "postgres" && c.Storage.DSN == "" {
		errs = append(errs, errors.New("storage.dsn обязателен для postgres"))
	}
	if c.Observability.SampleRate < 0 || c.Observability.SampleRate > 1 {
		errs = append(errs, errors.New("observability.sample_rate должен быть от 0 до 1"))
	}
	if c.Observability.OTLPEndpoint != "" {
		endpoint, err := url.Parse(c.Observability.OTLPEndpoint)
		if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
			errs = append(errs, errors.New("observability.otlp_endpoint должен быть абсолютным URL"))
		}
	}
	for _, cidr := range c.Runtime.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Errorf("runtime.trusted_proxies содержит некорректный CIDR %q", cidr))
		}
	}

	routeIDs := map[string]bool{}
	for i, route := range c.Routes {
		prefix := fmt.Sprintf("routes[%d]", i)
		if route.ID == "" {
			errs = append(errs, fmt.Errorf("%s.id обязателен", prefix))
		}
		if routeIDs[route.ID] {
			errs = append(errs, fmt.Errorf("%s.id %q повторяется", prefix, route.ID))
		}
		routeIDs[route.ID] = true
		if route.PathPrefix != "" && !strings.HasPrefix(route.PathPrefix, "/") {
			errs = append(errs, fmt.Errorf("%s.path_prefix должен начинаться с /", prefix))
		}
		if len(route.UpstreamPool) == 0 && len(route.Upstreams) == 0 {
			errs = append(errs, fmt.Errorf("%s должен содержать upstream", prefix))
		}
		for _, raw := range route.Upstreams {
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				errs = append(errs, fmt.Errorf("%s содержит некорректный upstream %q", prefix, raw))
			}
		}
		upstreamIDs := map[string]bool{}
		for j, upstream := range route.UpstreamPool {
			item := fmt.Sprintf("%s.upstream_pool[%d]", prefix, j)
			parsed, err := url.Parse(upstream.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				errs = append(errs, fmt.Errorf("%s содержит некорректный URL %q", item, upstream.URL))
			}
			if upstream.Weight < 1 {
				errs = append(errs, fmt.Errorf("%s.weight должен быть больше 0", item))
			}
			if upstreamIDs[upstream.ID] {
				errs = append(errs, fmt.Errorf("%s.id %q повторяется", item, upstream.ID))
			}
			upstreamIDs[upstream.ID] = true
		}
		if !slices.Contains([]string{"round_robin", "weighted_round_robin", "random", "least_connections", "consistent_hash"}, route.Balance.Strategy) {
			errs = append(errs, fmt.Errorf("%s.balance.strategy не поддерживается", prefix))
		}
		if route.Balance.Strategy == "consistent_hash" && route.Balance.HashKey == "" {
			errs = append(errs, fmt.Errorf("%s.balance.hash_key обязателен", prefix))
		}
		if route.MaxBodyBytes < 0 || route.MaxConcurrent < 0 {
			errs = append(errs, fmt.Errorf("%s содержит отрицательный лимит", prefix))
		}
		if route.Retry.Attempts < 1 || route.Retry.Attempts > 10 {
			errs = append(errs, fmt.Errorf("%s.retry.attempts должен быть от 1 до 10", prefix))
		}
	}

	policyIDs := map[string]bool{}
	for i, policy := range c.Policies {
		prefix := fmt.Sprintf("policies[%d]", i)
		if policy.ID == "" {
			errs = append(errs, fmt.Errorf("%s.id обязателен", prefix))
		}
		if policyIDs[policy.ID] {
			errs = append(errs, fmt.Errorf("%s.id %q повторяется", prefix, policy.ID))
		}
		policyIDs[policy.ID] = true
		if policy.Mode != ModeBlock && policy.Mode != ModeShadow {
			errs = append(errs, fmt.Errorf("%s.mode должен быть block или shadow", prefix))
		}
		if !slices.Contains([]string{ActionAllow, ActionDeny, ActionRateLimit, ActionRewrite, ActionRedirect, ActionAuthenticate}, policy.Action) {
			errs = append(errs, fmt.Errorf("%s.action не поддерживается", prefix))
		}
		if policy.Action == ActionRateLimit && (policy.Limit == nil || policy.Limit.Requests <= 0) {
			errs = append(errs, fmt.Errorf("%s.limit.requests должен быть больше 0", prefix))
		}
		if policy.Action == ActionRewrite && policy.Rewrite == nil {
			errs = append(errs, fmt.Errorf("%s.rewrite обязателен", prefix))
		}
		if policy.Action == ActionRedirect && policy.Redirect == nil {
			errs = append(errs, fmt.Errorf("%s.redirect обязателен", prefix))
		}
		if policy.Action == ActionAuthenticate && policy.Auth == nil {
			errs = append(errs, fmt.Errorf("%s.auth обязателен", prefix))
		}
		validateMatch(prefix+".match", &policy.Match, &errs)
		validateAuth(prefix, policy.Auth, &errs)
	}
	return errors.Join(errs...)
}

func validateMatch(prefix string, match *Match, errs *[]error) {
	for _, cidr := range match.SourceCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			*errs = append(*errs, fmt.Errorf("%s содержит некорректный CIDR %q", prefix, cidr))
		}
	}
	validateStringMatch(prefix+".path", match.Path, errs)
	for i := range match.HeaderMatches {
		validateStringMatch(fmt.Sprintf("%s.header_matches[%d]", prefix, i), &match.HeaderMatches[i].Match, errs)
	}
	for i := range match.QueryMatches {
		validateStringMatch(fmt.Sprintf("%s.query_matches[%d]", prefix, i), &match.QueryMatches[i].Match, errs)
	}
	for i := range match.All {
		validateMatch(fmt.Sprintf("%s.all[%d]", prefix, i), &match.All[i], errs)
	}
	for i := range match.Any {
		validateMatch(fmt.Sprintf("%s.any[%d]", prefix, i), &match.Any[i], errs)
	}
	if match.Not != nil {
		validateMatch(prefix+".not", match.Not, errs)
	}
}

func validateStringMatch(prefix string, match *StringMatch, errs *[]error) {
	if match == nil {
		return
	}
	count := 0
	for _, value := range []string{match.Exact, match.Prefix, match.Suffix, match.Regex} {
		if value != "" {
			count++
		}
	}
	if match.Absent {
		count++
	}
	if count != 1 {
		*errs = append(*errs, fmt.Errorf("%s должен содержать ровно один оператор", prefix))
	}
	if match.Regex != "" {
		if _, err := regexp.Compile(match.Regex); err != nil {
			*errs = append(*errs, fmt.Errorf("%s.regex: %w", prefix, err))
		}
	}
}

func validateAuth(prefix string, auth *Auth, errs *[]error) {
	if auth == nil {
		return
	}
	switch auth.Type {
	case "api_key":
		if auth.Value == "" {
			*errs = append(*errs, fmt.Errorf("%s.auth.value обязателен", prefix))
		}
	case "basic":
		if auth.Username == "" || auth.Password == "" {
			*errs = append(*errs, fmt.Errorf("%s.auth требует username и password", prefix))
		}
	case "jwt_hs256":
		if len(auth.JWTSecret) < 32 {
			*errs = append(*errs, fmt.Errorf("%s.auth.jwt_secret должен содержать минимум 32 символа", prefix))
		}
	default:
		*errs = append(*errs, fmt.Errorf("%s.auth.type не поддерживается", prefix))
	}
}
