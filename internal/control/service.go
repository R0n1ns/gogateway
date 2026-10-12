// Package control координирует persistence и атомарную публикацию config snapshot.
package control

import (
	"context"
	"fmt"
	"sync"

	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/storage"
)

type Service struct {
	mu         sync.RWMutex
	runtime    *gateway.Runtime
	repository storage.Repository
}

func New(runtime *gateway.Runtime, repository storage.Repository) *Service {
	return &Service{runtime: runtime, repository: repository}
}
func (s *Service) Current(ctx context.Context) (storage.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repository.Current(ctx)
}
func (s *Service) Get(ctx context.Context, revision int64) (storage.Snapshot, error) {
	return s.repository.Get(ctx, revision)
}
func (s *Service) List(ctx context.Context, limit int) ([]storage.Snapshot, error) {
	return s.repository.List(ctx, limit)
}
func (s *Service) Audit(ctx context.Context, limit int) ([]storage.AuditEvent, error) {
	return s.repository.Audit(ctx, limit)
}
func (s *Service) Apply(ctx context.Context, input *config.Config, expected int64, actor, comment string) (storage.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := input.DeepCopy()
	raw.Defaults()
	raw.Normalize()
	prepared, err := gateway.Prepare(raw)
	if err != nil {
		return storage.Snapshot{}, err
	}
	defer prepared.Close()
	snapshot, err := s.repository.Save(ctx, raw, expected, actor, comment)
	if err != nil {
		return storage.Snapshot{}, err
	}
	s.runtime.Publish(prepared, snapshot.Revision)
	return snapshot, nil
}
func (s *Service) Rollback(ctx context.Context, target, expected int64, actor string) (storage.Snapshot, error) {
	snapshot, err := s.repository.Get(ctx, target)
	if err != nil {
		return storage.Snapshot{}, err
	}
	return s.Apply(ctx, snapshot.Config, expected, actor, fmt.Sprintf("rollback to revision %d", target))
}
func (s *Service) RecordAudit(ctx context.Context, event storage.AuditEvent) error {
	return s.repository.RecordAudit(ctx, event)
}

func (s *Service) Close() error { return s.repository.Close() }
