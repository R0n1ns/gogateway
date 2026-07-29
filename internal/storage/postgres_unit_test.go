package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/R0n1ns/gogateway/internal/config"
)

func mockPostgres(t *testing.T) (*PostgresRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &PostgresRepository{db: db}, mock
}

func snapshotRow(revision int64, document []byte, created time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "document", "actor", "comment", "created_at"}).AddRow(revision, document, "tester", "change", created)
}

func TestPostgresReadMethods(t *testing.T) {
	repo, mock := mockPostgres(t)
	created := time.Unix(100, 0)
	document := []byte(`{"version":1}`)
	mock.ExpectQuery("SELECT s.id,s.document").WillReturnRows(snapshotRow(3, document, created))
	current, err := repo.Current(context.Background())
	if err != nil || current.Revision != 3 || current.Config.Revision != 3 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	mock.ExpectQuery("SELECT id,document.*config_snapshots WHERE id=\\$1").WithArgs(int64(2)).WillReturnRows(snapshotRow(2, document, created))
	item, err := repo.Get(context.Background(), 2)
	if err != nil || item.Actor != "tester" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	rows := sqlmock.NewRows([]string{"id", "document", "actor", "comment", "created_at"}).AddRow(2, document, "a", "c", created).AddRow(1, document, "b", "d", created)
	mock.ExpectQuery("SELECT id,document.*ORDER BY id DESC LIMIT \\$1").WithArgs(50).WillReturnRows(rows).RowsWillBeClosed()
	items, err := repo.List(context.Background(), 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSaveSuccess(t *testing.T) {
	repo, mock := mockPostgres(t)
	created := time.Unix(200, 0)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT current_snapshot_id.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"current_snapshot_id"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT nextval").WillReturnRows(sqlmock.NewRows([]string{"nextval"}).AddRow(int64(2)))
	mock.ExpectQuery("INSERT INTO config_snapshots").WithArgs(int64(2), 1, sqlmock.AnyArg(), "tester", "comment").WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(created))
	mock.ExpectExec("UPDATE config_state").WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO audit_events").WithArgs("tester", int64(2), "comment").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	saved, err := repo.Save(context.Background(), &config.Config{Version: 1}, 1, "tester", "comment")
	if err != nil || saved.Revision != 2 || saved.Config.Revision != 2 || !saved.CreatedAt.Equal(created) {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSaveConflicts(t *testing.T) {
	cases := []struct {
		name     string
		current  any
		expected int64
	}{{"stale", int64(2), 1}, {"missing-current", nil, 1}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := mockPostgres(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT current_snapshot_id.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"current_snapshot_id"}).AddRow(tc.current))
			mock.ExpectRollback()
			_, err := repo.Save(context.Background(), &config.Config{Version: 1}, tc.expected, "tester", "")
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("err=%v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresAuditAndMigrate(t *testing.T) {
	repo, mock := mockPostgres(t)
	mock.ExpectExec(regexp.QuoteMeta(postgresSchema)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("INSERT INTO audit_events").WithArgs("auth.denied", "tester", int64(4), "GET /api").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repo.RecordAudit(context.Background(), AuditEvent{Action: "auth.denied", Actor: "tester", Revision: 4, Details: "GET /api"}); err != nil {
		t.Fatal(err)
	}
	created := time.Unix(300, 0)
	rows := sqlmock.NewRows([]string{"id", "action", "actor", "revision", "details", "created_at"}).AddRow(1, "config.apply", "tester", 4, "change", created)
	mock.ExpectQuery("SELECT id,action.*ORDER BY id DESC LIMIT \\$1").WithArgs(100).WillReturnRows(rows).RowsWillBeClosed()
	events, err := repo.Audit(context.Background(), 0)
	if err != nil || len(events) != 1 || events[0].Revision != 4 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresScanAndSQLErrors(t *testing.T) {
	repo, mock := mockPostgres(t)
	mock.ExpectQuery("SELECT id,document.*config_snapshots WHERE id=\\$1").WithArgs(int64(99)).WillReturnError(sql.ErrNoRows)
	if _, err := repo.Get(context.Background(), 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	mock.ExpectQuery("SELECT id,document.*config_snapshots WHERE id=\\$1").WithArgs(int64(1)).WillReturnRows(snapshotRow(1, []byte(`{`), time.Now()))
	if _, err := repo.Get(context.Background(), 1); err == nil {
		t.Fatal("invalid JSON принят")
	}
	mock.ExpectQuery("SELECT s.id,s.document").WillReturnError(errors.New("database unavailable"))
	if _, err := repo.Current(context.Background()); err == nil {
		t.Fatal("SQL error потерян")
	}
}

func TestPostgresCloseAndMemoryFactory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectClose()
	repo := &PostgresRepository{db: db}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	memory, err := Open(context.Background(), &config.Config{Storage: config.Storage{Driver: "memory"}})
	if err != nil || memory == nil {
		t.Fatalf("memory=%v err=%v", memory, err)
	}
	if err := memory.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryListRecordAuditAndClose(t *testing.T) {
	repo := NewMemory(&config.Config{Version: 1})
	if _, err := repo.Save(context.Background(), &config.Config{Version: 1}, 1, "tester", "second"); err != nil {
		t.Fatal(err)
	}
	items, err := repo.List(context.Background(), 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err := repo.RecordAudit(context.Background(), AuditEvent{Action: "manual", Actor: "tester"}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.Audit(context.Background(), 1)
	if err != nil || len(events) != 1 || events[0].Action != "manual" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestScanSnapshotValidJSON(t *testing.T) {
	document, _ := json.Marshal(&config.Config{Version: 1})
	repo, mock := mockPostgres(t)
	mock.ExpectQuery("SELECT id,document.*config_snapshots WHERE id=\\$1").WithArgs(int64(7)).WillReturnRows(snapshotRow(7, document, time.Now()))
	item, err := repo.Get(context.Background(), 7)
	if err != nil || item.Config.Version != 1 {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}
