package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/R0n1ns/gogateway/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresRepository struct{ db *sql.DB }

func NewPostgres(ctx context.Context, settings config.Storage) (*PostgresRepository, error) {
	db, err := sql.Open("pgx", settings.DSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(settings.MaxOpenConns)
	db.SetMaxIdleConns(settings.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("подключиться к PostgreSQL: %w", err)
	}
	repo := &PostgresRepository{db: db}
	if err = repo.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}
func (r *PostgresRepository) Migrate(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, postgresSchema)
	if err != nil {
		return fmt.Errorf("применить PostgreSQL schema: %w", err)
	}
	return nil
}
func (r *PostgresRepository) Close() error { return r.db.Close() }
func (r *PostgresRepository) Current(ctx context.Context) (Snapshot, error) {
	row := r.db.QueryRowContext(ctx, `SELECT s.id,s.document,s.actor,s.comment,s.created_at FROM config_state st JOIN config_snapshots s ON s.id=st.current_snapshot_id WHERE st.singleton=TRUE`)
	return scanSnapshot(row)
}
func (r *PostgresRepository) Get(ctx context.Context, revision int64) (Snapshot, error) {
	return scanSnapshot(r.db.QueryRowContext(ctx, `SELECT id,document,actor,comment,created_at FROM config_snapshots WHERE id=$1`, revision))
}
func (r *PostgresRepository) List(ctx context.Context, limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,document,actor,comment,created_at FROM config_snapshots ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		item, scanErr := scanSnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *PostgresRepository) Save(ctx context.Context, cfg *config.Config, expected int64, actor, comment string) (Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var current sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT current_snapshot_id FROM config_state WHERE singleton=TRUE FOR UPDATE`).Scan(&current); err != nil {
		return Snapshot{}, err
	}
	if value := current.Int64; current.Valid && value != expected || !current.Valid && expected != 0 {
		return Snapshot{}, ErrConflict
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence('config_snapshots','id'))`).Scan(&revision); err != nil {
		return Snapshot{}, err
	}
	copy := cfg.DeepCopy()
	copy.Revision = revision
	document, err := json.Marshal(copy)
	if err != nil {
		return Snapshot{}, err
	}
	var created time.Time
	if err = tx.QueryRowContext(ctx, `INSERT INTO config_snapshots(id,version,document,actor,comment) VALUES($1,$2,$3,$4,$5) RETURNING created_at`, revision, copy.Version, document, actor, comment).Scan(&created); err != nil {
		return Snapshot{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE config_state SET current_snapshot_id=$1 WHERE singleton=TRUE`, revision); err != nil {
		return Snapshot{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(action,actor,revision,details) VALUES('config.apply',$1,$2,$3)`, actor, revision, comment); err != nil {
		return Snapshot{}, err
	}
	if err = tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Revision: revision, Config: copy, Actor: actor, Comment: comment, CreatedAt: created}, nil
}
func (r *PostgresRepository) RecordAudit(ctx context.Context, event AuditEvent) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events(action,actor,revision,details) VALUES($1,$2,$3,$4)`, event.Action, event.Actor, event.Revision, event.Details)
	return err
}

func (r *PostgresRepository) Audit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,action,actor,revision,details,created_at FROM audit_events ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var item AuditEvent
		if err = rows.Scan(&item.ID, &item.Action, &item.Actor, &item.Revision, &item.Details, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanSnapshot(row scanner) (Snapshot, error) {
	var item Snapshot
	var document []byte
	if err := row.Scan(&item.Revision, &document, &item.Actor, &item.Comment, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, ErrNotFound
		}
		return Snapshot{}, err
	}
	if err := json.Unmarshal(document, &item.Config); err != nil {
		return Snapshot{}, err
	}
	item.Config.Revision = item.Revision
	return item, nil
}

const postgresSchema = `
CREATE TABLE IF NOT EXISTS config_snapshots (id BIGSERIAL PRIMARY KEY,version INTEGER NOT NULL,document JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
ALTER TABLE config_snapshots ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT 'system';
ALTER TABLE config_snapshots ADD COLUMN IF NOT EXISTS comment TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS config_state (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),current_snapshot_id BIGINT REFERENCES config_snapshots(id));
INSERT INTO config_state(singleton,current_snapshot_id) VALUES(TRUE,(SELECT max(id) FROM config_snapshots)) ON CONFLICT(singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS audit_events (id BIGSERIAL PRIMARY KEY,action TEXT NOT NULL,actor TEXT NOT NULL,revision BIGINT NOT NULL,details TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS config_snapshots_created_at_idx ON config_snapshots(created_at DESC);
CREATE INDEX IF NOT EXISTS audit_events_created_at_idx ON audit_events(created_at DESC);`
