package simulator

import (
	"strings"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestSimulatorDefaultsNotFoundAndValidate(t *testing.T) {
	service, err := New(&config.Config{Routes: []config.Route{{ID: "api", PathPrefix: "/api"}}})
	if err != nil {
		t.Fatal(err)
	}
	missing := service.Simulate(Request{})
	if missing.Error == "" || missing.Decision.Status != 404 {
		t.Fatalf("missing=%+v", missing)
	}
	if err := missing.Validate(); err == nil || !strings.Contains(err.Error(), "simulation") {
		t.Fatalf("err=%v", err)
	}
	valid := service.Simulate(Request{RouteID: "api"})
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSimulatorRejectsInvalidRequestAndPolicy(t *testing.T) {
	if _, err := New(&config.Config{Policies: []config.Policy{{ID: "bad", Match: config.Match{Path: &config.StringMatch{Regex: "["}}}}}); err == nil {
		t.Fatal("invalid policy принят")
	}
	service, _ := New(&config.Config{Routes: []config.Route{{ID: "api", PathPrefix: "/"}}})
	result := service.Simulate(Request{Host: "bad host%", Path: "/%"})
	if result.Error == "" {
		t.Fatalf("result=%+v", result)
	}
}
