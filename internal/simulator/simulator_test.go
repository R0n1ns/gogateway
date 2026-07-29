package simulator

import (
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestSimulatorSelectsRouteAndExplainsPolicy(t *testing.T) {
	cfg := &config.Config{Routes: []config.Route{{ID: "api", PathPrefix: "/api"}}, Policies: []config.Policy{{ID: "deny", Mode: config.ModeBlock, Action: config.ActionDeny, Match: config.Match{Methods: []string{"DELETE"}}}}}
	service, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	result := service.Simulate(Request{Method: "DELETE", Host: "example", Path: "/api/users", SourceIP: "203.0.113.1"})
	if result.RouteID != "api" || result.Decision.Allowed || result.Decision.PolicyID != "deny" {
		t.Fatalf("result=%+v", result)
	}
}
