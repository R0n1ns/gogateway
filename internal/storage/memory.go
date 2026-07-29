package storage

import (
	"context"
	"sync"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
)

// MemoryRepository обеспечивает те же revision semantics без внешней БД.
type MemoryRepository struct {
	mu        sync.RWMutex
	snapshots []Snapshot
	audit     []AuditEvent
}

func NewMemory(initial *config.Config) *MemoryRepository {
	repo := &MemoryRepository{}
	if initial != nil {
		cfg := initial.DeepCopy()
		revision := cfg.Revision
		if revision < 1 {
			revision = 1
		}
		cfg.Revision = revision
		repo.snapshots = append(repo.snapshots, Snapshot{Revision: revision, Config: cfg, Actor: "system", Comment: "initial configuration", CreatedAt: time.Now().UTC()})
	}
	return repo
}
func (r *MemoryRepository) Current(_ context.Context) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.snapshots) == 0 {
		return Snapshot{}, ErrNotFound
	}
	return cloneSnapshot(r.snapshots[len(r.snapshots)-1]), nil
}
func (r *MemoryRepository) Get(_ context.Context, revision int64) (Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.snapshots {
		if item.Revision == revision {
			return cloneSnapshot(item), nil
		}
	}
	return Snapshot{}, ErrNotFound
}
func (r *MemoryRepository) List(_ context.Context, limit int) ([]Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > len(r.snapshots) {
		limit = len(r.snapshots)
	}
	out := make([]Snapshot, 0, limit)
	for i := len(r.snapshots) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, cloneSnapshot(r.snapshots[i]))
	}
	return out, nil
}
func (r *MemoryRepository) Save(_ context.Context, cfg *config.Config, expected int64, actor, comment string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := int64(0)
	if len(r.snapshots) > 0 {
		current = r.snapshots[len(r.snapshots)-1].Revision
	}
	if current != expected {
		return Snapshot{}, ErrConflict
	}
	next := current + 1
	copy := cfg.DeepCopy()
	copy.Revision = next
	snapshot := Snapshot{Revision: next, Config: copy, Actor: actor, Comment: comment, CreatedAt: time.Now().UTC()}
	r.snapshots = append(r.snapshots, snapshot)
	r.audit = append(r.audit, AuditEvent{ID: int64(len(r.audit) + 1), Action: "config.apply", Actor: actor, Revision: next, Details: comment, CreatedAt: snapshot.CreatedAt})
	return cloneSnapshot(snapshot), nil
}
func (r *MemoryRepository) Audit(_ context.Context, limit int) ([]AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > len(r.audit) {
		limit = len(r.audit)
	}
	out := make([]AuditEvent, 0, limit)
	for i := len(r.audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.audit[i])
	}
	return out, nil
}
func (r *MemoryRepository) RecordAudit(_ context.Context, event AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.ID = int64(len(r.audit) + 1)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	r.audit = append(r.audit, event)
	return nil
}

func (r *MemoryRepository) Close() error     { return nil }
func cloneSnapshot(source Snapshot) Snapshot { source.Config = source.Config.DeepCopy(); return source }
