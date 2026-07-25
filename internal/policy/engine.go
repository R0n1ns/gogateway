// Package policy сопоставляет HTTP-запросы с правилами доступа.
package policy

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"net"
	"net/http"
	"slices"
	"strings"
)

type Decision struct {
	Allowed          bool
	PolicyID, Action string
	Shadow           []string
	Limit            *config.Limit
}
type Engine struct{ rules []compiled }
type compiled struct {
	rule config.Policy
	nets []*net.IPNet
}

func New(rules []config.Policy) *Engine {
	e := &Engine{}
	for _, r := range rules {
		c := compiled{rule: r}
		for _, s := range r.Match.SourceCIDRs {
			_, n, _ := net.ParseCIDR(s)
			c.nets = append(c.nets, n)
		}
		e.rules = append(e.rules, c)
	}
	return e
}
func (e *Engine) Evaluate(r *http.Request, routeID, ip string) Decision {
	d := Decision{Allowed: true}
	for _, c := range e.rules {
		if !c.matches(r, routeID, net.ParseIP(ip)) {
			continue
		}
		if c.rule.Mode == config.ModeShadow {
			if c.rule.Action != config.ActionAllow {
				d.Shadow = append(d.Shadow, c.rule.ID)
			}
			continue
		}
		d.PolicyID = c.rule.ID
		d.Action = c.rule.Action
		if c.rule.Action == config.ActionDeny {
			d.Allowed = false
			return d
		}
		if c.rule.Action == config.ActionRateLimit {
			d.Limit = c.rule.Limit
			return d
		}
		if c.rule.Action == config.ActionAllow {
			return d
		}
	}
	return d
}
func (c compiled) matches(r *http.Request, routeID string, ip net.IP) bool {
	m := c.rule.Match
	if len(m.Methods) > 0 && !slices.Contains(m.Methods, r.Method) {
		return false
	}
	if m.PathPrefix != "" && !(r.URL.Path == m.PathPrefix || strings.HasPrefix(r.URL.Path, m.PathPrefix+"/")) {
		return false
	}
	if len(m.RouteIDs) > 0 && !slices.Contains(m.RouteIDs, routeID) {
		return false
	}
	if len(c.nets) > 0 {
		ok := false
		for _, n := range c.nets {
			if n.Contains(ip) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	for k, v := range m.Headers {
		if r.Header.Get(k) != v {
			return false
		}
	}
	for k, v := range m.Query {
		if r.URL.Query().Get(k) != v {
			return false
		}
	}
	if m.MinBodyBytes > 0 && r.ContentLength < m.MinBodyBytes {
		return false
	}
	if m.MaxBodyBytes > 0 && (r.ContentLength < 0 || r.ContentLength > m.MaxBodyBytes) {
		return false
	}
	return true
}
