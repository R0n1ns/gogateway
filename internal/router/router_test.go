package router

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"net/http/httptest"
	"testing"
)

func TestMostSpecificRoute(t *testing.T) {
	x := New([]config.Route{{ID: "all", PathPrefix: "/"}, {ID: "api", Host: "example.com", PathPrefix: "/api"}})
	r := httptest.NewRequest("GET", "http://example.com/api/users", nil)
	v, ok := x.Match(r)
	if !ok || v.ID != "api" {
		t.Fatalf("route=%+v ok=%v", v, ok)
	}
	r = httptest.NewRequest("GET", "http://example.com/apix", nil)
	v, _ = x.Match(r)
	if v.ID != "all" {
		t.Fatalf("boundary: %+v", v)
	}
}
