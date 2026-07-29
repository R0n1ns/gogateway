// Package storage хранит immutable snapshots control plane.
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

var ErrNotFound = errors.New("snapshot не найден")
var ErrConflict = errors.New("revision conflict")

type Snapshot struct {
	Revision  int64          `json:"revision"`
	Config    *config.Config `json:"config"`
	Actor     string         `json:"actor"`
	Comment   string         `json:"comment"`
	CreatedAt time.Time      `json:"created_at"`
}
type AuditEvent struct {
	ID        int64     `json:"id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Revision  int64     `json:"revision"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// Repository задаёт persistence contract независимо от PostgreSQL.
type Repository interface {
	Current(context.Context) (Snapshot, error)
	Get(context.Context, int64) (Snapshot, error)
	List(context.Context, int) ([]Snapshot, error)
	Save(context.Context, *config.Config, int64, string, string) (Snapshot, error)
	Audit(context.Context, int) ([]AuditEvent, error)
	Close() error
}
