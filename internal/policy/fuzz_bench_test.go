package policy

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func FuzzStringMatcherPrefix(f *testing.F) {
	f.Add("/api/users", "/api")
	f.Add("hello", "^h.*o$")
	f.Fuzz(func(t *testing.T, value, pattern string) {
		if len(pattern) > 256 || len(value) > 2048 || pattern == "" {
			return
		}
		matcher := config.StringMatch{Prefix: pattern}
		if got := matchString(value, matcher, nil); got != strings.HasPrefix(value, pattern) {
			t.Fatal("prefix mismatch")
		}
	})
}
func FuzzStringMatcherExact(f *testing.F) {
	f.Add("hello", "hello")
	f.Fuzz(func(t *testing.T, value, pattern string) {
		if len(value) > 2048 || len(pattern) > 256 || pattern == "" {
			return
		}
		if got := matchString(value, config.StringMatch{Exact: pattern}, nil); got != (value == pattern) {
			t.Fatal("exact mismatch")
		}
	})
}
func FuzzStringMatcherRegex(f *testing.F) {
	f.Add("hello", "^h.*o$")
	f.Add("x", "[")
	f.Fuzz(func(t *testing.T, value, pattern string) {
		if len(value) > 2048 || len(pattern) > 256 || pattern == "" {
			return
		}
		re, err := regexp.Compile(pattern)
		cfg := config.Policy{ID: "fuzz", Mode: config.ModeBlock, Action: config.ActionDeny, Match: config.Match{Path: &config.StringMatch{Regex: pattern}}}
		_, compileErr := Compile([]config.Policy{cfg})
		if err != nil {
			if compileErr == nil {
				t.Fatal("invalid regex accepted")
			}
			return
		}
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		if got := matchString(value, config.StringMatch{Regex: pattern}, re); got != re.MatchString(value) {
			t.Fatal("regex mismatch")
		}
	})
}
func BenchmarkEvaluate1000Policies(b *testing.B) {
	rules := make([]config.Policy, 1000)
	for i := range rules {
		rules[i] = config.Policy{ID: "rule", Mode: config.ModeBlock, Action: config.ActionDeny, Match: config.Match{PathPrefix: "/never"}}
	}
	engine, _ := Compile(rules)
	request := httptest.NewRequest("GET", "http://example/api", nil)
	b.ReportAllocs()
	for b.Loop() {
		engine.Evaluate(request, "api", "127.0.0.1")
	}
}
