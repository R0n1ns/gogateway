package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSequenceAndControlIsolation(t *testing.T) {
	f := &fixture{name: "test"}
	w := httptest.NewRecorder()
	f.control(w, httptest.NewRequest("PUT", "/behavior", strings.NewReader(`{"statuses":[503,200]}`)))
	for _, want := range []int{503, 200, 200} {
		w = httptest.NewRecorder()
		f.serve(w, httptest.NewRequest("POST", "/echo?x=1&x=2", strings.NewReader("body")))
		if w.Code != want {
			t.Fatalf("got %d want %d", w.Code, want)
		}
		var got record
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if string(got.Body) != "body" || got.RawQuery != "x=1&x=2" {
			t.Fatal(got)
		}
	}
	f.serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if f.count != 3 || f.active != 0 {
		t.Fatalf("count=%d active=%d", f.count, f.active)
	}
	f.serve(httptest.NewRecorder(), httptest.NewRequest("POST", "/reset", nil))
	if f.count != 4 {
		t.Fatal("public port exposed control")
	}
	f.control(httptest.NewRecorder(), httptest.NewRequest("POST", "/reset", nil))
	if f.count != 0 {
		t.Fatal("reset failed")
	}
}
