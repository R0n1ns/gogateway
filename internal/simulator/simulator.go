// Package simulator выполняет routing и policy evaluation без отправки запроса upstream.
package simulator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/policy"
	"github.com/R0n1ns/gogateway/internal/router"
)

type Request struct {
	Method    string            `json:"method"`
	Host      string            `json:"host"`
	Path      string            `json:"path"`
	SourceIP  string            `json:"source_ip"`
	Headers   map[string]string `json:"headers,omitempty"`
	Query     map[string]string `json:"query,omitempty"`
	BodyBytes int64             `json:"body_bytes,omitempty"`
	RouteID   string            `json:"route_id,omitempty"`
}
type Result struct {
	RouteID  string          `json:"route_id,omitempty"`
	Decision policy.Decision `json:"decision"`
	Error    string          `json:"error,omitempty"`
}
type Simulator struct {
	router *router.Router
	engine *policy.Engine
}

func New(cfg *config.Config) (*Simulator, error) {
	engine, err := policy.Compile(cfg.Policies)
	if err != nil {
		return nil, err
	}
	return &Simulator{router: router.New(cfg.Routes), engine: engine}, nil
}
func (s *Simulator) Simulate(input Request) Result {
	method := input.Method
	if method == "" {
		method = http.MethodGet
	}
	path := input.Path
	if path == "" {
		path = "/"
	}
	host := input.Host
	if host == "" {
		host = "gateway.local"
	}
	values := url.Values{}
	for key, value := range input.Query {
		values.Set(key, value)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, "http://"+host+path+"?"+values.Encode(), nil)
	if err != nil {
		return Result{Error: err.Error()}
	}
	request.Host = input.Host
	request.ContentLength = input.BodyBytes
	for key, value := range input.Headers {
		request.Header.Set(key, value)
	}
	routeID := input.RouteID
	if routeID == "" {
		route, ok := s.router.Match(request)
		if !ok {
			return Result{Decision: policy.Decision{Allowed: false, Status: http.StatusNotFound, Action: "route_not_found"}, Error: "маршрут не найден"}
		}
		routeID = route.ID
	}
	source := strings.TrimSpace(input.SourceIP)
	if source == "" {
		source = "127.0.0.1"
	}
	decision := s.engine.Evaluate(request, routeID, source)
	return Result{RouteID: routeID, Decision: decision}
}
func (r Result) Validate() error {
	if r.Error != "" {
		return fmt.Errorf("simulation: %s", r.Error)
	}
	return nil
}
