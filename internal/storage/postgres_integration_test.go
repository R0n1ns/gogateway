package storage

import (
	"github.com/R0n1ns/gogateway/internal/config"
	"os"
	"testing"
)

func TestPostgresRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("GATEFORGE_TEST_DATABASE")
	if dsn == "" {
		t.Skip("GATEFORGE_TEST_DATABASE не задан")
	}
	settings := config.Storage{Driver: "postgres", DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1}
	repo, err := NewPostgres(t.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	current, err := repo.Current(t.Context())
	expected := int64(0)
	if err == nil {
		expected = current.Revision
	} else if err != ErrNotFound {
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
