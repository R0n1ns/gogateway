package policytest

import (
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/simulator"
)

func TestRunReportsMismatch(t *testing.T) {
	allowed := false
	cfg := &config.Config{Routes: []config.Route{{ID: "api", PathPrefix: "/api"}}}
	results, err := Run(cfg, File{Cases: []Case{{Name: "must deny", Request: simulator.Request{Path: "/api"}, Expected: Expected{Allowed: &allowed}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Passed || results[0].Message == "" {
		t.Fatalf("results=%+v", results)
	}
}
