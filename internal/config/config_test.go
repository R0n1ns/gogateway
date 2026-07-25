package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	const in = `{"version":1,"public":{"address":":8080","read_timeout":"3s"},"admin":{"address":"127.0.0.1:9090"},"routes":[{"id":"api","host":"API.EXAMPLE.COM","upstreams":["http://backend:8080"]}],"policies":[{"id":"deny","priority":20,"action":"deny"},{"id":"allow","priority":10,"action":"allow","match":{"methods":["get"]}}]}`
	var c Config
	if err := json.Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Normalize()
	if c.Public.ReadTimeout.Duration != 3*time.Second || c.Routes[0].Host != "api.example.com" || c.Policies[0].ID != "allow" {
		t.Fatalf("не нормализовано: %#v", c)
	}
}
func TestValidation(t *testing.T) {
	c := Config{Public: Server{Address: ":1"}, Admin: AdminServer{Server: Server{Address: ":2"}}, Routes: []Route{{ID: "x", PathPrefix: "bad", Upstreams: []string{"ftp://bad"}}, {ID: "x"}}, Policies: []Policy{{ID: "p", Mode: "bad", Action: ActionRateLimit, Match: Match{SourceCIDRs: []string{"bad"}}}}}
	err := c.Validate()
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	for _, v := range []string{"version", "повторяется", "path_prefix", "upstream", "mode", "limit.requests", "CIDR"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("нет %s: %v", v, err)
		}
	}
}
