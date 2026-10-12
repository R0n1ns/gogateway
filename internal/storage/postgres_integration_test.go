//go:build integration

package storage

import (
	"errors"
	"os"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

func TestPostgresRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("GATEFORGE_TEST_DATABASE")
	if dsn == "" {
		t.Fatal("GATEFORGE_TEST_DATABASE обязателен для integration")
	}
	settings := config.Storage{Driver: "postgres", DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1}
	repo, err := NewPostgres(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	current, err := repo.Current(t.Context())
	expected := int64(0)
	if err == nil {
		expected = current.Revision
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: 1}
	saved, err := repo.Save(t.Context(), cfg, expected, "integration-test", "verify postgres repository")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Get(t.Context(), saved.Revision)
	if err != nil || loaded.Revision != saved.Revision {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}
