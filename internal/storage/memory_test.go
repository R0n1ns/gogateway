package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestMemoryRepositoryRevisionConflictAndIsolation(t *testing.T) {
	ctx := context.Background()
	initial := &config.Config{Version: 1}
	repo := NewMemory(initial)
	current, err := repo.Current(ctx)
	if err != nil || current.Revision != 1 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	next := &config.Config{Version: 1, AccessLog: config.AccessLog{Format: "json"}}
	saved, err := repo.Save(ctx, next, 1, "tester", "change")
	if err != nil || saved.Revision != 2 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	saved.Config.AccessLog.Format = "changed"
	loaded, _ := repo.Get(ctx, 2)
	if loaded.Config.AccessLog.Format != "json" {
		t.Fatal("repository вернул shared config")
	}
	if _, err = repo.Save(ctx, next, 1, "tester", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("error=%v", err)
	}
	audit, _ := repo.Audit(ctx, 10)
	if len(audit) != 1 || audit[0].Actor != "tester" {
		t.Fatalf("audit=%+v", audit)
	}
}
