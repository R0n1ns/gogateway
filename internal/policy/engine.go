// Package policy компилирует правила и принимает детерминированное решение по HTTP-запросу.
package policy

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/R0n1ns/gogateway/internal/config"
)

// Decision содержит итог и объяснение, пригодное для access log и Policy Simulator.
type Decision struct {
	Allowed     bool             `json:"allowed"`
	Status      int              `json:"status"`
	PolicyID    string           `json:"policy_id,omitempty"`
	Action      string           `json:"action,omitempty"`
	Shadow      []string         `json:"shadow,omitempty"`
	Limit       *config.Limit    `json:"limit,omitempty"`
	Rewrite     *config.Rewrite  `json:"rewrite,omitempty"`
	Redirect    *config.Redirect `json:"redirect,omitempty"`
	Subject     string           `json:"subject,omitempty"`
	Explanation []string         `json:"explanation,omitempty"`
}

// Engine неизменяем после компиляции и безопасен для конкурентного чтения.
type Engine struct{ rules []compiledPolicy }

type compiledPolicy struct {
	rule  config.Policy
	match compiledMatch
}

type compiledMatch struct {
	source      config.Match
	pathRegex   *regexp.Regexp
	headerRegex map[int]*regexp.Regexp
	queryRegex  map[int]*regexp.Regexp
	networks    []*net.IPNet
	all         []compiledMatch
	any         []compiledMatch
	not         *compiledMatch
}

// Compile заранее разбирает CIDR и regexp. Ошибка публикации правила возникает
// до атомарной замены runtime snapshot.
func Compile(rules []config.Policy) (*Engine, error) {
	engine := &Engine{rules: make([]compiledPolicy, 0, len(rules))}
	for _, rule := range rules {
		matcher, err := compileMatch(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("policy %q: %w", rule.ID, err)
		}
		engine.rules = append(engine.rules, compiledPolicy{rule: rule, match: matcher})
	}
	return engine, nil
}

// New сохранён для обратной совместимости с MVP. Валидная конфигурация не может
// привести к ошибке компиляции.
func New(rules []config.Policy) *Engine {
	engine, _ := Compile(rules)
	if engine == nil {
		return &Engine{}
	}
	return engine
}

// Evaluate применяет правила в порядке priority, заданном нормализацией config.
func (e *Engine) Evaluate(r *http.Request, routeID, sourceIP string) Decision {
	decision := Decision{Allowed: true, Status: http.StatusOK}
	ip := net.ParseIP(sourceIP)
	for _, compiled := range e.rules {
		matched, reasons := compiled.match.matches(r, routeID, ip)
		if !matched {
			continue
		}
		rule := compiled.rule
		if rule.Mode == config.ModeShadow {
			decision.Shadow = append(decision.Shadow, rule.ID)
			decision.Explanation = append(decision.Explanation, "shadow:"+rule.ID)
			continue
		}
		decision.PolicyID, decision.Action = rule.ID, rule.Action
		decision.Explanation = append(decision.Explanation, reasons...)
		switch rule.Action {
		case config.ActionDeny:
			decision.Allowed, decision.Status = false, http.StatusForbidden
			return decision
		case config.ActionRateLimit:
			decision.Limit = rule.Limit
			return decision
		case config.ActionRewrite:
			decision.Rewrite = rule.Rewrite
			continue
		case config.ActionRedirect:
			decision.Redirect, decision.Status = rule.Redirect, rule.Redirect.Status
			return decision
		case config.ActionAuthenticate:
			subject, err := Authenticate(r, rule.Auth)
			if err != nil {
				decision.Allowed, decision.Status = false, http.StatusUnauthorized
				decision.Explanation = append(decision.Explanation, "auth:"+err.Error())
				return decision
			}
			decision.Subject = subject
			continue
		case config.ActionAllow:
			return decision
		}
	}
	return decision
}

func compileMatch(source config.Match) (compiledMatch, error) {
	out := compiledMatch{source: source, headerRegex: map[int]*regexp.Regexp{}, queryRegex: map[int]*regexp.Regexp{}}
	var err error
	if source.Path != nil && source.Path.Regex != "" {
		if out.pathRegex, err = regexp.Compile(source.Path.Regex); err != nil {
			return out, err
		}
	}
	for i, item := range source.HeaderMatches {
		if item.Match.Regex != "" {
			out.headerRegex[i], err = regexp.Compile(item.Match.Regex)
			if err != nil {
				return out, err
			}
		}
	}
	for i, item := range source.QueryMatches {
		if item.Match.Regex != "" {
			out.queryRegex[i], err = regexp.Compile(item.Match.Regex)
			if err != nil {
				return out, err
			}
		}
	}
	for _, cidr := range source.SourceCIDRs {
		_, network, parseErr := net.ParseCIDR(cidr)
		if parseErr != nil {
			return out, parseErr
		}
		out.networks = append(out.networks, network)
	}
	for _, child := range source.All {
		compiled, compileErr := compileMatch(child)
		if compileErr != nil {
			return out, compileErr
		}
		out.all = append(out.all, compiled)
	}
	for _, child := range source.Any {
		compiled, compileErr := compileMatch(child)
		if compileErr != nil {
			return out, compileErr
		}
		out.any = append(out.any, compiled)
	}
	if source.Not != nil {
		compiled, compileErr := compileMatch(*source.Not)
		if compileErr != nil {
			return out, compileErr
		}
		out.not = &compiled
	}
	return out, nil
}

func (m compiledMatch) matches(r *http.Request, routeID string, ip net.IP) (bool, []string) {
	reasons := make([]string, 0, 8)
	source := m.source
	if len(source.Methods) > 0 {
		if !slices.Contains(source.Methods, r.Method) {
			return false, nil
		}
		reasons = append(reasons, "method")
	}
	if source.PathPrefix != "" {
		if !pathPrefix(r.URL.Path, source.PathPrefix) {
			return false, nil
		}
		reasons = append(reasons, "path_prefix")
	}
	if source.Path != nil {
		if !matchString(r.URL.Path, *source.Path, m.pathRegex) {
			return false, nil
		}
		reasons = append(reasons, "path")
	}
	if len(source.RouteIDs) > 0 {
		if !slices.Contains(source.RouteIDs, routeID) {
			return false, nil
		}
		reasons = append(reasons, "route_id")
	}
	if len(m.networks) > 0 {
		found := false
		for _, network := range m.networks {
			if network.Contains(ip) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
		reasons = append(reasons, "source_cidr")
	}
	for name, expected := range source.Headers {
		if r.Header.Get(name) != expected {
			return false, nil
		}
		reasons = append(reasons, "header:"+name)
	}
	for name, expected := range source.Query {
		if r.URL.Query().Get(name) != expected {
			return false, nil
		}
		reasons = append(reasons, "query:"+name)
	}
	for i, item := range source.HeaderMatches {
		values, exists := r.Header[http.CanonicalHeaderKey(item.Name)]
		value := strings.Join(values, ",")
		if item.Match.Absent {
			if exists {
				return false, nil
			}
		} else if !matchString(value, item.Match, m.headerRegex[i]) {
			return false, nil
		}
		reasons = append(reasons, "header_match:"+item.Name)
	}
	for i, item := range source.QueryMatches {
		values, exists := r.URL.Query()[item.Name]
		value := strings.Join(values, ",")
		if item.Match.Absent {
			if exists {
				return false, nil
			}
		} else if !matchString(value, item.Match, m.queryRegex[i]) {
			return false, nil
		}
		reasons = append(reasons, "query_match:"+item.Name)
	}
	if source.MinBodyBytes > 0 {
		if r.ContentLength < source.MinBodyBytes {
			return false, nil
		}
		reasons = append(reasons, "min_body_bytes")
	}
	if source.MaxBodyBytes > 0 {
		if r.ContentLength < 0 || r.ContentLength > source.MaxBodyBytes {
			return false, nil
		}
		reasons = append(reasons, "max_body_bytes")
	}
	for _, child := range m.all {
		matched, childReasons := child.matches(r, routeID, ip)
		if !matched {
			return false, nil
		}
		reasons = append(reasons, childReasons...)
	}
	if len(m.any) > 0 {
		found := false
		for _, child := range m.any {
			matched, childReasons := child.matches(r, routeID, ip)
			if matched {
				found = true
				reasons = append(reasons, childReasons...)
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	if m.not != nil {
		matched, _ := m.not.matches(r, routeID, ip)
		if matched {
			return false, nil
		}
		reasons = append(reasons, "not")
	}
	return true, reasons
}

func matchString(value string, matcher config.StringMatch, compiled *regexp.Regexp) bool {
	switch {
	case matcher.Absent:
		return value == ""
	case matcher.Exact != "":
		return value == matcher.Exact
	case matcher.Prefix != "":
		return strings.HasPrefix(value, matcher.Prefix)
	case matcher.Suffix != "":
		return strings.HasSuffix(value, matcher.Suffix)
	case compiled != nil:
		return compiled.MatchString(value)
	default:
		return false
	}
}

func pathPrefix(path, prefix string) bool {
	return prefix == "" || prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/")
}
