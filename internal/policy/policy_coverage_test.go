package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestAuthenticateAllCredentialTypes(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example", nil)
	if _, err := Authenticate(request, nil); err == nil {
		t.Fatal("nil auth принят")
	}
	if _, err := Authenticate(request, &config.Auth{Type: "other"}); err == nil {
		t.Fatal("unknown auth принят")
	}
	api := &config.Auth{Type: "api_key", Header: "X-Key", Value: "secret"}
	if _, err := Authenticate(request, api); err == nil {
		t.Fatal("неверный API key принят")
	}
	request.Header.Set("X-Key", "secret")
	if subject, err := Authenticate(request, api); err != nil || subject != "secret" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	basic := &config.Auth{Type: "basic", Username: "user", Password: "pass"}
	if _, err := Authenticate(request, basic); err == nil {
		t.Fatal("пустой Basic принят")
	}
	request.SetBasicAuth("user", "pass")
	if subject, err := Authenticate(request, basic); err != nil || subject != "user" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
}

func TestJWTValidationErrorBranches(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	now := time.Unix(1_000, 0)
	auth := &config.Auth{JWTSecret: secret, JWTIssuer: "issuer", JWTAudience: "api"}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	wrongHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))

	cases := []struct {
		name, token string
		config      *config.Auth
	}{
		{"parts", "bad", auth},
		{"header-base64", "!.e30.eA", auth},
		{"algorithm", wrongHeader + ".e30.eA", auth},
		{"signature-base64", header + ".e30.!", auth},
		{"signature", signedJWT("wrong-secret", `{"sub":"user"}`), auth},
		{"payload-base64", signSegments(secret, header, "!"), auth},
		{"claims-json", signedJWT(secret, `{`), auth},
		{"not-before", signedJWT(secret, `{"nbf":2000,"iss":"issuer","aud":"api"}`), auth},
		{"issuer", signedJWT(secret, `{"iss":"other","aud":"api"}`), auth},
		{"audience", signedJWT(secret, `{"iss":"issuer","aud":"other"}`), auth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifyJWT(tc.token, tc.config, now); err == nil {
				t.Fatal("ожидалась ошибка JWT")
			}
		})
	}
	validArrayAudience := signedJWT(secret, `{"sub":"user","iss":"issuer","aud":["other","api"],"exp":2000}`)
	if subject, err := verifyJWT(validArrayAudience, auth, now); err != nil || subject != "user" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
	if audienceContains([]any{12, "api"}, "api") != true || audienceContains(12, "api") {
		t.Fatal("audienceContains работает неверно")
	}
}

func signSegments(secret, header, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestCompileRejectsInvalidNestedMatchers(t *testing.T) {
	matches := []config.Match{
		{SourceCIDRs: []string{"bad"}},
		{Path: &config.StringMatch{Regex: "["}},
		{HeaderMatches: []config.NamedMatch{{Name: "X", Match: config.StringMatch{Regex: "["}}}},
		{QueryMatches: []config.NamedMatch{{Name: "q", Match: config.StringMatch{Regex: "["}}}},
		{All: []config.Match{{Path: &config.StringMatch{Regex: "["}}}},
		{Any: []config.Match{{Path: &config.StringMatch{Regex: "["}}}},
		{Not: &config.Match{Path: &config.StringMatch{Regex: "["}}},
	}
	for i, match := range matches {
		if _, err := Compile([]config.Policy{{ID: "bad", Match: match}}); err == nil {
			t.Fatalf("case %d принят", i)
		}
	}
	engine := New([]config.Policy{{ID: "bad", Match: config.Match{Path: &config.StringMatch{Regex: "["}}}})
	if engine == nil {
		t.Fatal("New должен вернуть безопасный пустой engine")
	}
}

func TestEvaluateActions(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example/api", nil)
	cases := []struct {
		name  string
		rule  config.Policy
		check func(Decision) bool
	}{
		{"allow", config.Policy{ID: "allow", Mode: config.ModeBlock, Action: config.ActionAllow}, func(d Decision) bool { return d.Allowed && d.PolicyID == "allow" }},
		{"limit", config.Policy{ID: "limit", Mode: config.ModeBlock, Action: config.ActionRateLimit, Limit: &config.Limit{Requests: 1}}, func(d Decision) bool { return d.Limit != nil }},
		{"redirect", config.Policy{ID: "redirect", Mode: config.ModeBlock, Action: config.ActionRedirect, Redirect: &config.Redirect{URL: "/login", Status: 307}}, func(d Decision) bool { return d.Redirect != nil && d.Status == 307 }},
		{"auth-fail", config.Policy{ID: "auth", Mode: config.ModeBlock, Action: config.ActionAuthenticate, Auth: &config.Auth{Type: "api_key", Header: "X-Key", Value: "secret"}}, func(d Decision) bool { return !d.Allowed && d.Status == 401 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := Compile([]config.Policy{tc.rule})
			if decision := engine.Evaluate(request, "api", "127.0.0.1"); !tc.check(decision) {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestMatchOperatorsAndMismatchBranches(t *testing.T) {
	type setup func(*http.Request)
	cases := []struct {
		name            string
		match           config.Match
		path, route, ip string
		length          int64
		setup           setup
		blocked         bool
	}{
		{"method-mismatch", config.Match{Methods: []string{"POST"}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"path-prefix-boundary", config.Match{PathPrefix: "/api"}, "/apix", "api", "127.0.0.1", 0, nil, false},
		{"path-exact", config.Match{Path: &config.StringMatch{Exact: "/api"}}, "/api", "api", "127.0.0.1", 0, nil, true},
		{"path-prefix", config.Match{Path: &config.StringMatch{Prefix: "/a"}}, "/api", "api", "127.0.0.1", 0, nil, true},
		{"path-suffix", config.Match{Path: &config.StringMatch{Suffix: ".json"}}, "/a.json", "api", "127.0.0.1", 0, nil, true},
		{"route-mismatch", config.Match{RouteIDs: []string{"other"}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"cidr-mismatch", config.Match{SourceCIDRs: []string{"10.0.0.0/8"}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"header-mismatch", config.Match{Headers: map[string]string{"X-Key": "yes"}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"query-mismatch", config.Match{Query: map[string]string{"role": "admin"}}, "/api?role=user", "api", "127.0.0.1", 0, nil, false},
		{"header-absent", config.Match{HeaderMatches: []config.NamedMatch{{Name: "X-Missing", Match: config.StringMatch{Absent: true}}}}, "/api", "api", "127.0.0.1", 0, nil, true},
		{"header-present-when-absent", config.Match{HeaderMatches: []config.NamedMatch{{Name: "X-Key", Match: config.StringMatch{Absent: true}}}}, "/api", "api", "127.0.0.1", 0, func(r *http.Request) { r.Header.Set("X-Key", "x") }, false},
		{"header-regex-mismatch", config.Match{HeaderMatches: []config.NamedMatch{{Name: "X-Key", Match: config.StringMatch{Regex: "^yes$"}}}}, "/api", "api", "127.0.0.1", 0, func(r *http.Request) { r.Header.Set("X-Key", "no") }, false},
		{"query-absent", config.Match{QueryMatches: []config.NamedMatch{{Name: "missing", Match: config.StringMatch{Absent: true}}}}, "/api", "api", "127.0.0.1", 0, nil, true},
		{"query-present-when-absent", config.Match{QueryMatches: []config.NamedMatch{{Name: "q", Match: config.StringMatch{Absent: true}}}}, "/api?q=x", "api", "127.0.0.1", 0, nil, false},
		{"min-body", config.Match{MinBodyBytes: 10}, "/api", "api", "127.0.0.1", 5, nil, false},
		{"max-body-unknown", config.Match{MaxBodyBytes: 10}, "/api", "api", "127.0.0.1", -1, nil, false},
		{"all-mismatch", config.Match{All: []config.Match{{Methods: []string{"POST"}}}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"any-mismatch", config.Match{Any: []config.Match{{Methods: []string{"POST"}}}}, "/api", "api", "127.0.0.1", 0, nil, false},
		{"not-mismatch", config.Match{Not: &config.Match{Methods: []string{"GET"}}}, "/api", "api", "127.0.0.1", 0, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://example"+tc.path, strings.NewReader("body"))
			request.ContentLength = tc.length
			if tc.setup != nil {
				tc.setup(request)
			}
			engine, err := Compile([]config.Policy{{ID: "deny", Mode: config.ModeBlock, Action: config.ActionDeny, Match: tc.match}})
			if err != nil {
				t.Fatal(err)
			}
			blocked := !engine.Evaluate(request, tc.route, tc.ip).Allowed
			if blocked != tc.blocked {
				t.Fatalf("blocked=%v want=%v", blocked, tc.blocked)
			}
		})
	}
	if !pathPrefix("/api/users", "/api") || pathPrefix("/apix", "/api") || !pathPrefix("/anything", "/") {
		t.Fatal("pathPrefix boundary неверен")
	}
}
