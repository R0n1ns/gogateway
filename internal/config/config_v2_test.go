package config

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownField(t *testing.T) {
	_, err := Decode([]byte(`{"version":1,"public":{"address":":8080"},"admin":{"address":":9090"},"storage":{"driver":"memory"},"routes":[],"policies":[],"access_log":{},"typo":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Decode() error = %v", err)
	}
}

func TestDeepCopyDoesNotShareMutableData(t *testing.T) {
	original := &Config{Routes: []Route{{AddHeaders: map[string]string{"X-Test": "one"}, Upstreams: []string{"http://one"}}}, Policies: []Policy{{Match: Match{Headers: map[string]string{"X-Key": "one"}}}}}
	copy := original.DeepCopy()
	copy.Routes[0].AddHeaders["X-Test"] = "two"
	copy.Routes[0].Upstreams[0] = "http://two"
	copy.Policies[0].Match.Headers["X-Key"] = "two"
	if original.Routes[0].AddHeaders["X-Test"] != "one" || original.Routes[0].Upstreams[0] != "http://one" || original.Policies[0].Match.Headers["X-Key"] != "one" {
		t.Fatal("DeepCopy разделяет mutable данные")
	}
}

func TestEnvironmentExpansion(t *testing.T) {
	t.Setenv("GATEFORGE_TEST_KEY", "secret-value")
	cfg := &Config{Admin: AdminServer{APIKey: "${GATEFORGE_TEST_KEY}"}, Storage: Storage{DSN: "prefix-${GATEFORGE_TEST_KEY}"}}
	cfg.ExpandEnvironment()
	if cfg.Admin.APIKey != "secret-value" || cfg.Storage.DSN != "prefix-secret-value" {
		t.Fatalf("env не раскрыт: %#v", cfg)
	}
	_ = os.Unsetenv("GATEFORGE_UNUSED")
}

func TestValidateLogicalRegex(t *testing.T) {
	cfg := validV2Config()
	cfg.Policies = []Policy{{ID: "regex", Mode: ModeBlock, Action: ActionDeny, Match: Match{Any: []Match{{Path: &StringMatch{Regex: "["}}}}}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "regex") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func validV2Config() *Config {
	cfg := &Config{Version: 1, Public: Server{Address: ":8080"}, Admin: AdminServer{Server: Server{Address: ":9090"}}, Storage: Storage{Driver: "memory"}, Routes: []Route{{ID: "api", PathPrefix: "/", UpstreamPool: []Upstream{{ID: "one", URL: "http://127.0.0.1:8081", Weight: 1}}, Balance: Balance{Strategy: "round_robin"}, Retry: Retry{Attempts: 1}}}}
	cfg.Defaults()
	cfg.Normalize()
	return cfg
}

func TestDecodeKeepsSecretReferenceRaw(t *testing.T) {
	t.Setenv("GATEFORGE_RAW_SECRET", "resolved-secret")
	cfg, err := Decode([]byte(`{"version":1,"public":{"address":":8080"},"admin":{"address":":9090","api_key":"${GATEFORGE_RAW_SECRET}"},"storage":{"driver":"memory"},"routes":[],"policies":[],"access_log":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Admin.APIKey != "${GATEFORGE_RAW_SECRET}" {
		t.Fatalf("secret сохранён раскрытым: %q", cfg.Admin.APIKey)
	}
	resolved := cfg.DeepCopy()
	resolved.ExpandEnvironment()
	if resolved.Admin.APIKey != "resolved-secret" {
		t.Fatalf("secret не раскрыт runtime: %q", resolved.Admin.APIKey)
	}
}
