package policytest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/policy"
	"github.com/R0n1ns/gogateway/internal/simulator"
)

func TestLoadAndCompareAllFields(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(valid, []byte(`{"cases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, err := Load(valid); err != nil || len(file.Cases) != 0 {
		t.Fatalf("file=%+v err=%v", file, err)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file принят")
	}
	invalid := filepath.Join(dir, "invalid.json")
	_ = os.WriteFile(invalid, []byte(`{`), 0o600)
	if _, err := Load(invalid); err == nil {
		t.Fatal("invalid JSON принят")
	}
	allowed := false
	actual := simulator.Result{RouteID: "api", Decision: policy.Decision{Allowed: false, Status: 403, Action: "deny", PolicyID: "policy"}}
	if message := compare(Expected{RouteID: "api", Allowed: &allowed, Status: 403, Action: "deny", PolicyID: "policy"}, actual); message != "" {
		t.Fatal(message)
	}
	checks := []Expected{{RouteID: "other"}, {Allowed: boolPointer(true)}, {Status: 401}, {Action: "allow"}, {PolicyID: "other"}}
	for _, expected := range checks {
		if message := compare(expected, actual); message == "" {
			t.Fatalf("ожидалось несовпадение для %+v", expected)
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestRunReturnsCompileError(t *testing.T) {
	cfg := &config.Config{Policies: []config.Policy{{ID: "bad", Match: config.Match{Path: &config.StringMatch{Regex: "["}}}}}
	_, err := Run(cfg, File{})
	if err == nil || !strings.Contains(err.Error(), "error parsing regexp") {
		t.Fatalf("err=%v", err)
	}
}
