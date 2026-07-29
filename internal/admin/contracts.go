package admin

import (
	"context"
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/storage"
	"github.com/R0n1ns/gogateway/internal/upstream"
	"net/http"
)

// DataPlane — минимальный read/control contract, необходимый административному API.
type DataPlane interface {
	Config() *config.Config
	Metrics() http.Handler
	Ready() bool
	UpstreamStatus() map[string][]upstream.Status
}

// ControlPlane скрывает конкретное memory/PostgreSQL хранилище от HTTP handler.
type ControlPlane interface {
	Current(context.Context) (storage.Snapshot, error)
	Get(context.Context, int64) (storage.Snapshot, error)
	List(context.Context, int) ([]storage.Snapshot, error)
	Apply(context.Context, *config.Config, int64, string, string) (storage.Snapshot, error)
	Rollback(context.Context, int64, int64, string) (storage.Snapshot, error)
	Audit(context.Context, int) ([]storage.AuditEvent, error)
	RecordAudit(context.Context, storage.AuditEvent) error
}
