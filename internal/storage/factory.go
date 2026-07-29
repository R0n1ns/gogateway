package storage

import (
	"context"
	"errors"
	"github.com/R0n1ns/gogateway/internal/config"
)

// Open создаёт выбранный repository и гарантирует наличие initial snapshot.
func Open(ctx context.Context, cfg *config.Config) (Repository, error) {
	var repository Repository
	var err error
	switch cfg.Storage.Driver {
	case "postgres":
		resolved := cfg.DeepCopy()
		resolved.ExpandEnvironment()
		repository, err = NewPostgres(ctx, resolved.Storage)
	default:
		repository = NewMemory(cfg)
	}
	if err != nil {
		return nil, err
	}
	if _, err = repository.Current(ctx); errors.Is(err, ErrNotFound) {
		if _, err = repository.Save(ctx, cfg, 0, "system", "initial configuration"); err != nil {
			_ = repository.Close()
			return nil, err
		}
	} else if err != nil {
		_ = repository.Close()
		return nil, err
	}
	return repository, nil
}
