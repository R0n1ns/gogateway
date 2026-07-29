package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestLogicalMatchAndExplanation(t *testing.T) {
	rules := []config.Policy{{ID: "complex", Mode: config.ModeBlock, Action: config.ActionDeny, Match: config.Match{All: []config.Match{{Methods: []string{"POST"}}, {Any: []config.Match{{Path: &config.StringMatch{Regex: `^/admin/\d+$`}}, {Headers: map[string]string{"X-Admin": "true"}}}}, {Not: &config.Match{SourceCIDRs: []string{"10.0.0.0/8"}}}}}}}
	engine, err := Compile(rules)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://example/admin/42", nil)
	decision := engine.Evaluate(request, "admin", "203.0.113.2")
	if decision.Allowed || decision.PolicyID != "complex" {
		t.Fatalf("decision = %+v", decision)
	}
	if len(decision.Explanation) < 3 {
		t.Fatalf("нет объяснения: %+v", decision.Explanation)
	}
}

func TestRewriteContinuesToAuthentication(t *testing.T) {
	rules := []config.Policy{
		{ID: "rewrite", Mode: config.ModeBlock, Action: config.ActionRewrite, Rewrite: &config.Rewrite{PathPrefix: "/v2"}},
		{ID: "auth", Mode: config.ModeBlock, Action: config.ActionAuthenticate, Auth: &config.Auth{Type: "api_key", Header: "X-Key", Value: "secret"}},
	}
	engine, _ := Compile(rules)
	request := httptest.NewRequest("GET", "http://example/api", nil)
	request.Header.Set("X-Key", "secret")
	decision := engine.Evaluate(request, "api", "127.0.0.1")
	if !decision.Allowed || decision.Rewrite == nil || decision.Subject != "secret" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestJWTAuthentication(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	auth := &config.Auth{Type: "jwt_hs256", Header: "Authorization", JWTSecret: secret, JWTIssuer: "gateforge", JWTAudience: "api"}
	request := httptest.NewRequest("GET", "http://example", nil)
	payload := `{"sub":"user-42","iss":"gateforge","aud":"api","exp":` + itoa64(time.Now().Add(time.Minute).Unix()) + `}`
	request.Header.Set("Authorization", "Bearer "+signedJWT(secret, payload))
	subject, err := Authenticate(request, auth)
	if err != nil || subject != "user-42" {
		t.Fatalf("subject=%q err=%v", subject, err)
	}
}

func TestJWTRejectsExpiredToken(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	_, err := verifyJWT(signedJWT(secret, `{"sub":"user","exp":1}`), &config.Auth{JWTSecret: secret}, time.Now())
	if err == nil {
		t.Fatal("истёкший JWT принят")
	}
}

func signedJWT(secret, payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(header + "." + body))
	return header + "." + body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	if negative {
		result = "-" + result
	}
	return result
}
