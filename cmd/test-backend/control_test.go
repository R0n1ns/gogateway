package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlClientDoesNotFollowRedirects(t *testing.T) {
	var calls atomic.Int64
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer external.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	response, err := newControlClient().Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusTemporaryRedirect || calls.Load() != 0 {
		t.Fatal("control client followed a redirect")
	}
}

func TestHeldRequestIsReleasedExplicitly(t *testing.T) {
	f := &fixture{behavior: behavior{Hold: true}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get(server.URL)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	active := false
	for time.Now().Before(deadline) {
		f.mu.Lock()
		active = f.active == 1
		f.mu.Unlock()
		if active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !active {
		t.Fatal("request never became active")
	}
	select {
	case <-done:
		t.Fatal("held request completed before release")
	default:
	}
	f.control(httptest.NewRecorder(), httptest.NewRequest("POST", "/release", nil))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
