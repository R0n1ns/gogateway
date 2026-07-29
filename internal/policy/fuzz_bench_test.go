package policy

import (
	"net/http/httptest"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func FuzzStringMatcher(f *testing.F) {
	f.Add("/api/users", "/api")
	f.Add("hello", "^h.*o$")
	f.Fuzz(func(t *testing.T, value, pattern string) {
		if len(pattern) > 256 || len(value) > 2048 {
			return
		}
		matcher := config.StringMatch{Prefix: pattern}
		_ = matchString(value, matcher, nil)
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
