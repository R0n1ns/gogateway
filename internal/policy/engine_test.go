package policy

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"net/http/httptest"
	"testing"
)

func TestEvaluateBlockAndShadow(t *testing.T) {
	e := New([]config.Policy{{ID: "shadow", Mode: "shadow", Action: "deny", Match: config.Match{Headers: map[string]string{"X-Debug": "1"}}}, {ID: "deny", Mode: "block", Action: "deny", Match: config.Match{Methods: []string{"DELETE"}, SourceCIDRs: []string{"10.0.0.0/8"}}}})
	r := httptest.NewRequest("DELETE", "http://x/api", nil)
	r.Header.Set("X-Debug", "1")
	d := e.Evaluate(r, "api", "10.1.2.3")
	if d.Allowed || d.PolicyID != "deny" || len(d.Shadow) != 1 {
		t.Fatalf("decision=%+v", d)
	}
}
func TestHeaderAndQueryMismatch(t *testing.T) {
	e := New([]config.Policy{{ID: "deny", Mode: "block", Action: "deny", Match: config.Match{Headers: map[string]string{"X-Key": "yes"}, Query: map[string]string{"role": "admin"}}}})
	r := httptest.NewRequest("GET", "http://x/?role=user", nil)
	r.Header.Set("X-Key", "yes")
	if !e.Evaluate(r, "", "127.0.0.1").Allowed {
		t.Fatal("не должно совпасть")
	}
}
