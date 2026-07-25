// Package router выбирает наиболее специфичный маршрут.
package router

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"net"
	"net/http"
	"strings"
)

type Router struct{ routes []config.Route }

func New(routes []config.Route) *Router { return &Router{routes: routes} }
func (x *Router) Match(r *http.Request) (config.Route, bool) {
	host := strings.ToLower(r.Host)
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	best := -1
	var out config.Route
	for _, v := range x.routes {
		if v.Host != "" && v.Host != host {
			continue
		}
		if !pathMatch(r.URL.Path, v.PathPrefix) {
			continue
		}
		if len(v.PathPrefix) > best {
			best = len(v.PathPrefix)
			out = v
		}
	}
	return out, best >= 0
}
func pathMatch(path, prefix string) bool {
	if prefix == "" || prefix == "/" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
