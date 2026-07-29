package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSuccessAndErrors(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"version":1,"public":{"address":":8080"},"admin":{"address":":9090"},"storage":{"driver":"memory"},"routes":[],"policies":[],"access_log":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(valid); err != nil || cfg.Version != 1 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil || !strings.Contains(err.Error(), "прочитать") {
		t.Fatalf("missing error=%v", err)
	}
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(invalid); err == nil || !strings.Contains(err.Error(), "разобрать") {
		t.Fatalf("invalid error=%v", err)
	}
}

func TestDecodeRejectsSecondDocument(t *testing.T) {
	input := []byte(`{"version":1,"public":{"address":":1"},"admin":{"address":":2"},"routes":[],"policies":[]} {}`)
	if _, err := Decode(input); err == nil {
		t.Fatal("второй JSON-документ должен быть запрещён")
	}
}

func TestDefaultsAndNormalizeAllSections(t *testing.T) {
	cfg := Config{
		Routes: []Route{{ID: "api", Host: "EXAMPLE.TEST", Upstreams: []string{"http://one", "http://two", "http://three", "http://four", "http://five", "http://six", "http://seven", "http://eight", "http://nine", "http://ten"}, HealthCheck: HealthCheck{Enabled: true}, Retry: Retry{Attempts: 2, Methods: []string{"get"}}, CircuitBreaker: CircuitBreaker{FailureThreshold: 2}}},
		Policies: []Policy{
			{ID: "limit", Priority: 30, Action: ActionRateLimit, Limit: &Limit{Requests: 1}},
			{ID: "redirect", Priority: 20, Action: ActionRedirect, Redirect: &Redirect{URL: "/login"}},
			{ID: "auth", Priority: 10, Action: ActionAuthenticate, Auth: &Auth{Type: "api_key", Value: "key"}, Match: Match{All: []Match{{Methods: []string{"post"}}}, Any: []Match{{Methods: []string{"put"}}}, Not: &Match{Methods: []string{"delete"}}}},
		},
	}
	cfg.Defaults()
	cfg.Normalize()
	if cfg.Storage.Driver != "memory" || cfg.Runtime.MaxConfigBytes == 0 || cfg.Observability.ServiceName != "gateforge" {
		t.Fatalf("root defaults не заполнены: %+v", cfg)
	}
	route := cfg.Routes[0]
	if route.Host != "example.test" || route.PathPrefix != "/" || route.UpstreamTimeout.Duration != 30*time.Second {
		t.Fatalf("route defaults=%+v", route)
	}
	if route.HealthCheck.Path != "/healthz" || route.HealthCheck.HealthyThreshold != 2 || route.HealthCheck.UnhealthyThreshold != 3 {
		t.Fatalf("health defaults=%+v", route.HealthCheck)
	}
	if route.Retry.Backoff.Duration == 0 || len(route.Retry.Statuses) == 0 || route.Retry.Methods[0] != "GET" {
		t.Fatalf("retry defaults=%+v", route.Retry)
	}
	if route.CircuitBreaker.OpenTimeout.Duration == 0 || route.CircuitBreaker.HalfOpenRequests != 1 || route.UpstreamPool[9].ID != "api-10" {
		t.Fatalf("circuit/upstream defaults=%+v %+v", route.CircuitBreaker, route.UpstreamPool[9])
	}
	if cfg.Policies[0].ID != "auth" || cfg.Policies[0].Auth.Header != "Authorization" || cfg.Policies[1].Redirect.Status != 302 {
		t.Fatalf("policy defaults=%+v", cfg.Policies)
	}
	if cfg.Policies[0].Match.All[0].Methods[0] != "POST" || cfg.Policies[0].Match.Any[0].Methods[0] != "PUT" || cfg.Policies[0].Match.Not.Methods[0] != "DELETE" {
		t.Fatalf("nested normalize=%+v", cfg.Policies[0].Match)
	}
}

func TestExpandEnvironmentCoversAllCredentialFields(t *testing.T) {
	t.Setenv("GF_VALUE", "resolved")
	cfg := Config{Admin: AdminServer{APIKey: "${GF_VALUE}"}, Storage: Storage{DSN: "postgres://${GF_VALUE}"}, Routes: []Route{{Upstreams: []string{"http://${GF_VALUE}"}, UpstreamPool: []Upstream{{URL: "http://${GF_VALUE}"}}}}, Policies: []Policy{{Auth: &Auth{Value: "${GF_VALUE}", Username: "${GF_VALUE}", Password: "${GF_VALUE}", JWTSecret: "${GF_VALUE}"}}, {}}}
	cfg.ExpandEnvironment()
	if cfg.Admin.APIKey != "resolved" || cfg.Storage.DSN != "postgres://resolved" || cfg.Routes[0].Upstreams[0] != "http://resolved" || cfg.Routes[0].UpstreamPool[0].URL != "http://resolved" {
		t.Fatalf("env expansion=%+v", cfg)
	}
	auth := cfg.Policies[0].Auth
	if auth.Value != "resolved" || auth.Username != "resolved" || auth.Password != "resolved" || auth.JWTSecret != "resolved" {
		t.Fatalf("auth expansion=%+v", auth)
	}
}

func TestDeepCopyCoversNestedPointers(t *testing.T) {
	original := &Config{Runtime: Runtime{TrustedProxies: []string{"127.0.0.0/8"}}, Routes: []Route{{UpstreamPool: []Upstream{{ID: "one"}}, RemoveHeaders: []string{"X-One"}, AddResponseHeaders: map[string]string{"X-One": "1"}, Retry: Retry{Methods: []string{"GET"}, Statuses: []int{503}}}}, Policies: []Policy{{Match: Match{Path: &StringMatch{Prefix: "/api"}, SourceCIDRs: []string{"10.0.0.0/8"}, RouteIDs: []string{"api"}, Query: map[string]string{"q": "1"}, HeaderMatches: []NamedMatch{{Name: "X"}}, QueryMatches: []NamedMatch{{Name: "q"}}, All: []Match{{Methods: []string{"GET"}}}, Any: []Match{{Methods: []string{"POST"}}}, Not: &Match{Methods: []string{"DELETE"}}}, Limit: &Limit{Requests: 1}, Rewrite: &Rewrite{SetHeaders: map[string]string{"X": "1"}}, Redirect: &Redirect{URL: "/"}, Auth: &Auth{Type: "api_key"}}}}
	copy := original.DeepCopy()
	copy.Runtime.TrustedProxies[0] = "changed"
	copy.Routes[0].RemoveHeaders[0] = "changed"
	copy.Routes[0].AddResponseHeaders["X-One"] = "2"
	copy.Routes[0].Retry.Methods[0] = "POST"
	copy.Policies[0].Match.Path.Prefix = "/changed"
	copy.Policies[0].Match.Not.Methods[0] = "PATCH"
	copy.Policies[0].Rewrite.SetHeaders["X"] = "2"
	copy.Policies[0].Redirect.URL = "/changed"
	copy.Policies[0].Auth.Type = "basic"
	if original.Runtime.TrustedProxies[0] == "changed" || original.Routes[0].RemoveHeaders[0] == "changed" || original.Policies[0].Match.Path.Prefix == "/changed" || original.Policies[0].Rewrite.SetHeaders["X"] == "2" {
		t.Fatal("DeepCopy разделяет nested данные")
	}
	if (*Config)(nil).DeepCopy() != nil {
		t.Fatal("nil DeepCopy должен вернуть nil")
	}
}

func TestDurationJSONBranches(t *testing.T) {
	data, err := json.Marshal(Duration{Duration: 1500 * time.Millisecond})
	if err != nil || string(data) != `"1.5s"` {
		t.Fatalf("marshal=%s err=%v", data, err)
	}
	var duration Duration
	for _, input := range []string{`12`, `"bad"`, `"-1s"`} {
		if err := json.Unmarshal([]byte(input), &duration); err == nil {
			t.Fatalf("для %s ожидалась ошибка", input)
		}
	}
}

func TestValidationCoversInvalidSections(t *testing.T) {
	cfg := validV2Config()
	cfg.Storage = Storage{Driver: "postgres"}
	cfg.Observability.SampleRate = 2
	cfg.Observability.OTLPEndpoint = "://bad"
	cfg.Runtime.TrustedProxies = []string{"bad"}
	cfg.Routes = append(cfg.Routes, Route{ID: "broken", PathPrefix: "/", MaxBodyBytes: -1, MaxConcurrent: -1, UpstreamPool: []Upstream{{ID: "same", URL: "ftp://bad", Weight: 0}, {ID: "same", URL: "http://ok", Weight: 1}}, Balance: Balance{Strategy: "consistent_hash"}, Retry: Retry{Attempts: 11}})
	cfg.Policies = []Policy{
		{ID: "duplicate", Mode: ModeBlock, Action: ActionRewrite}, {ID: "duplicate", Mode: ModeBlock, Action: ActionRedirect},
		{ID: "auth-api", Mode: ModeBlock, Action: ActionAuthenticate, Auth: &Auth{Type: "api_key"}}, {ID: "auth-basic", Mode: ModeBlock, Action: ActionAuthenticate, Auth: &Auth{Type: "basic"}}, {ID: "auth-jwt", Mode: ModeBlock, Action: ActionAuthenticate, Auth: &Auth{Type: "jwt_hs256", JWTSecret: "short"}}, {ID: "auth-other", Mode: ModeBlock, Action: ActionAuthenticate, Auth: &Auth{Type: "other"}},
		{ID: "matcher", Mode: ModeBlock, Action: ActionDeny, Match: Match{Path: &StringMatch{Exact: "/", Prefix: "/"}, HeaderMatches: []NamedMatch{{Name: "X", Match: StringMatch{}}}, QueryMatches: []NamedMatch{{Name: "q", Match: StringMatch{Regex: "["}}}}},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("ожидались ошибки validation")
	}
	for _, expected := range []string{"storage.dsn", "sample_rate", "otlp_endpoint", "trusted_proxies", "weight", "hash_key", "retry.attempts", "rewrite", "redirect", "auth.value", "username", "jwt_secret", "auth.type", "ровно один оператор", "regex"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("нет ошибки %q в %v", expected, err)
		}
	}
}
