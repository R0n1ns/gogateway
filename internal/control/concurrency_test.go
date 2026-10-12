package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/storage"
)

type delayedRepository struct {
	storage.Repository
	saved   chan struct{}
	release chan struct{}
	second  chan struct{}
	fail    bool
}

func (r *delayedRepository) Save(ctx context.Context, cfg *config.Config, expected int64, actor, comment string) (storage.Snapshot, error) {
	if r.fail {
		return storage.Snapshot{}, errors.New("injected storage failure")
	}
	snapshot, err := r.Repository.Save(ctx, cfg, expected, actor, comment)
	if expected == 1 {
		close(r.saved)
		<-r.release
	} else {
		close(r.second)
	}
	return snapshot, err
}

func TestConcurrentPublication(t *testing.T) {
	cfg := controlConfig("http://127.0.0.1:1")
	runtime, err := gateway.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	repo := &delayedRepository{Repository: storage.NewMemory(cfg), saved: make(chan struct{}), release: make(chan struct{}), second: make(chan struct{})}
	service := New(runtime, repo)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, e := service.Apply(t.Context(), cfg, 1, "one", ""); e != nil {
			t.Error(e)
		}
	}()
	<-repo.saved
	go func() {
		defer wg.Done()
		if _, e := service.Apply(t.Context(), cfg, 2, "two", ""); e != nil {
			t.Error(e)
		}
	}()
	select {
	case <-repo.second:
	case <-time.After(100 * time.Millisecond):
	}
	close(repo.release)
	wg.Wait()
	current, err := repo.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Config().Revision != current.Revision {
		t.Fatalf("runtime=%d storage=%d", runtime.Config().Revision, current.Revision)
	}
}

func TestSaveFailureDoesNotPublish(t *testing.T) {
	cfg := controlConfig("http://127.0.0.1:1")
	runtime, err := gateway.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	service := New(runtime, &delayedRepository{Repository: storage.NewMemory(cfg), fail: true})
	cfg.Routes[0].PathPrefix = "/changed"
	if _, err = service.Apply(t.Context(), cfg, 1, "test", ""); err == nil {
		t.Fatal("expected failure")
	}
	if runtime.Config().Revision != 1 || runtime.Config().Routes[0].PathPrefix != "/" {
		t.Fatal("failed write published")
	}
}
