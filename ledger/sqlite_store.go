//go:build ledger_sqlite

package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	_ "modernc.org/sqlite"
)

// sqliteDriverName is the database/sql driver name modernc.org/sqlite
// registers itself under.
const sqliteDriverName = "sqlite"

// sqliteMaxConns bounds the connection pool. SQLite-family engines
// serialize writers regardless of pool size; a bounded pool plus the
// busy_timeout pragma keeps a concurrent writer waiting its turn
// instead of failing immediately with SQLITE_BUSY.
const sqliteMaxConns = 8

// pragmaDSNParams applies once per pooled connection at open time.
const pragmaDSNParams = "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// sqlitePragmas are executed once per connection immediately after
// open, for correctness before the pool has cycled a new connection
// through the DSN-level _pragma= params above.
var sqlitePragmas = []string{
	"PRAGMA journal_mode=WAL",
	"PRAGMA synchronous=NORMAL",
	"PRAGMA foreign_keys=ON",
	"PRAGMA busy_timeout=5000",
}

// sqliteDSN builds the database/sql DSN for path. The driver splits a
// DSN at the first literal '?' to separate the filename from its
// query parameters, so a path containing '?' is escaped through a
// file: URI instead of truncating to the wrong filename.
func sqliteDSN(path string) string {
	if strings.Contains(path, "?") {
		return "file:" + url.PathEscape(path) + "?" + pragmaDSNParams
	}
	return path + "?" + pragmaDSNParams
}

// SQLiteStore is a Store backed by a local SQLite database file (or
// ":memory:") through modernc.org/sqlite, reached over database/sql.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens path (a file path, or ":memory:" for an
// in-process database with no file) through modernc.org/sqlite,
// creates the ledger_tasks schema if absent, sets the connection pool
// size and the WAL/synchronous/foreign-keys/busy-timeout pragmas, and
// Pings once to fail fast on a bad path or a permissions error.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open(sqliteDriverName, sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("ledger: open sqlite store: %w", err)
	}
	db.SetMaxOpenConns(sqliteMaxConns)
	db.SetMaxIdleConns(sqliteMaxConns)
	for _, p := range sqlitePragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("ledger: sqlite pragma %q: %w", p, err)
		}
	}
	if _, err := db.Exec(createLedgerTasksTable); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ledger: create ledger_tasks table: %w", err)
	}
	if err := migrateAuditColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ledger: ping sqlite store: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// migrateAuditColumns adds any of auditColumns absent from an
// already-existing ledger_tasks table, backfilling the empty string
// for every pre-existing row. A fresh table already declares all four
// columns through createLedgerTasksTable, so this runs zero ALTER
// TABLE statements on first open; running it again against an
// already-migrated file is equally a no-op.
func migrateAuditColumns(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(ledger_tasks)")
	if err != nil {
		return fmt.Errorf("ledger: sqlite table_info: %w", err)
	}
	present := map[string]bool{}
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			dfltValue  any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("ledger: sqlite table_info scan: %w", err)
		}
		present[name] = true
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("ledger: sqlite table_info close: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: sqlite table_info rows: %w", err)
	}
	for _, name := range auditColumns {
		if present[name] {
			continue
		}
		stmt := "ALTER TABLE ledger_tasks ADD COLUMN " + name + " TEXT NOT NULL DEFAULT ''"
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ledger: sqlite add column %q: %w", name, err)
		}
	}
	return nil
}

// Close closes the underlying *sql.DB. Idempotent.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// sqlScanner is the shared method *sql.Row and *sql.Rows both
// implement, letting scanTaskState serve Load and Range alike.
type sqlScanner interface {
	Scan(dest ...any) error
}

// encodeNeeds marshals a Needs slice to the JSON form SQLiteStore
// stores in its needs column. A nil slice encodes as "[]", never
// "null", so decodeNeeds always gets valid JSON back.
func encodeNeeds(needs []IdempotencyKey) ([]byte, error) {
	if needs == nil {
		needs = []IdempotencyKey{}
	}
	return json.Marshal(needs)
}

// decodeNeeds parses a needs column value back into a Needs slice.
func decodeNeeds(data []byte) ([]IdempotencyKey, error) {
	var needs []IdempotencyKey
	if err := json.Unmarshal(data, &needs); err != nil {
		return nil, fmt.Errorf("ledger: decode needs: %w", err)
	}
	return needs, nil
}

// encodeTask marshals a TaskState.Task value to the JSON form
// SQLiteStore stores in its task BLOB column. A value
// encoding/json.Marshal cannot encode (a channel, a function, an
// unexported-field-only struct) returns a non-nil error here.
func encodeTask(task any) ([]byte, error) {
	b, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("ledger: encode task: %w", err)
	}
	return b, nil
}

// decodeTask parses a task BLOB column value back into an any value.
// An empty column decodes to nil with no error.
func decodeTask(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var task any
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("ledger: decode task: %w", err)
	}
	return task, nil
}

// scanTaskState reads one ledger_tasks row into a TaskState.
func scanTaskState(row sqlScanner) (TaskState, error) {
	var (
		key, status, owner, leaseUntil, blockedBy string
		needsJSON                                 string
		sequence, fence, rev                      uint64
		taskBytes                                 []byte
		createdAtStr, updatedAtStr                string
		createdBy, updatedBy                      string
	)
	if err := row.Scan(&key, &status, &sequence, &owner, &fence, &leaseUntil, &needsJSON, &blockedBy, &taskBytes, &rev,
		&createdAtStr, &updatedAtStr, &createdBy, &updatedBy); err != nil {
		return TaskState{}, err
	}
	needs, err := decodeNeeds([]byte(needsJSON))
	if err != nil {
		return TaskState{}, err
	}
	task, err := decodeTask(taskBytes)
	if err != nil {
		return TaskState{}, err
	}
	var lu time.Time
	if leaseUntil != "" {
		lu, err = time.Parse(time.RFC3339Nano, leaseUntil)
		if err != nil {
			return TaskState{}, fmt.Errorf("ledger: parse lease_until: %w", err)
		}
	}
	createdAt, err := parseAuditTime(createdAtStr, "created_at")
	if err != nil {
		return TaskState{}, err
	}
	updatedAt, err := parseAuditTime(updatedAtStr, "updated_at")
	if err != nil {
		return TaskState{}, err
	}
	return TaskState{
		Key:        IdempotencyKey(key),
		Status:     machine.Status(status),
		Sequence:   Sequence(sequence),
		Owner:      OwnerID(owner),
		Fence:      FenceToken(fence),
		LeaseUntil: lu,
		Needs:      needs,
		BlockedBy:  IdempotencyKey(blockedBy),
		Task:       task,
		Rev:        rev,
		CreatedBy:  Actor(createdBy),
		CreatedAt:  createdAt,
		UpdatedBy:  Actor(updatedBy),
		UpdatedAt:  updatedAt,
	}, nil
}

// parseAuditTime parses a created_at/updated_at column value. An
// empty column (a never-written or migration-backfilled row) reads
// back as the zero time.Time.
func parseAuditTime(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("ledger: parse %s: %w", field, err)
	}
	return t, nil
}

// Load returns the stored record for key. The bool reports whether a
// record exists; a missing row maps to found == false with no error.
func (s *SQLiteStore) Load(ctx context.Context, key IdempotencyKey) (TaskState, bool, error) {
	if err := ctx.Err(); err != nil {
		return TaskState{}, false, err
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+selectLedgerTaskColumns+" FROM ledger_tasks WHERE key = ?", string(key))
	ts, err := scanTaskState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskState{}, false, nil
	}
	if err != nil {
		return TaskState{}, false, fmt.Errorf("ledger: sqlite load: %w", err)
	}
	return ts, true, nil
}

// CompareAndSwap compares old against the stored record's (Sequence,
// Status, Fence, Rev) tuple and, on a match, stores new with Rev set
// to one more than the prior stored Rev. A zero-value old against an
// absent key inserts new at Rev zero. Any other mismatch fails with
// ok false and no error. Each branch is one atomic SQL statement;
// SQLite's busy_timeout pragma serializes a concurrent writer instead
// of failing it immediately.
func (s *SQLiteStore) CompareAndSwap(ctx context.Context, key IdempotencyKey, old TaskState, new TaskState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	needsJSON, err := encodeNeeds(new.Needs)
	if err != nil {
		return false, err
	}
	taskBytes, err := encodeTask(new.Task)
	if err != nil {
		return false, err
	}
	var leaseUntil string
	if !new.LeaseUntil.IsZero() {
		leaseUntil = new.LeaseUntil.Format(time.RFC3339Nano)
	}
	createdAt := formatAuditTime(new.CreatedAt)
	updatedAt := formatAuditTime(new.UpdatedAt)
	if old.Sequence == 0 && old.Status == "" && old.Fence == 0 && old.Rev == 0 {
		res, err := s.db.ExecContext(ctx, insertLedgerTask,
			string(key), string(new.Status), uint64(new.Sequence), string(new.Owner),
			uint64(new.Fence), leaseUntil, string(needsJSON), string(new.BlockedBy), taskBytes,
			createdAt, updatedAt, string(new.CreatedBy), string(new.UpdatedBy))
		if err != nil {
			return false, fmt.Errorf("ledger: sqlite insert: %w", err)
		}
		return rowsAffectedOne(res)
	}
	res, err := s.db.ExecContext(ctx, updateLedgerTask,
		string(new.Status), uint64(new.Sequence), string(new.Owner), uint64(new.Fence),
		leaseUntil, string(needsJSON), string(new.BlockedBy), taskBytes,
		updatedAt, string(new.UpdatedBy),
		string(key), uint64(old.Sequence), string(old.Status), uint64(old.Fence), uint64(old.Rev))
	if err != nil {
		return false, fmt.Errorf("ledger: sqlite update: %w", err)
	}
	return rowsAffectedOne(res)
}

// formatAuditTime formats a CreatedAt/UpdatedAt value for storage. A
// zero time.Time formats as the empty string, matching leaseUntil's
// own zero-value convention.
func formatAuditTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// rowsAffectedOne reports whether res affected exactly one row.
func rowsAffectedOne(res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("ledger: sqlite rows affected: %w", err)
	}
	return n == 1, nil
}

// Range calls fn once per stored record, in no defined order. It
// materializes every row into a Go slice before closing the
// *sql.Rows cursor, then calls fn once per row after the cursor is
// closed. It stops early when fn returns false; fn must not call
// back into the SQLiteStore.
func (s *SQLiteStore) Range(ctx context.Context, fn func(TaskState) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+selectLedgerTaskColumns+" FROM ledger_tasks")
	if err != nil {
		return fmt.Errorf("ledger: sqlite range query: %w", err)
	}
	var records []TaskState
	for rows.Next() {
		ts, err := scanTaskState(rows)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("ledger: sqlite range scan: %w", err)
		}
		records = append(records, ts)
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		return fmt.Errorf("ledger: sqlite range close: %w", err)
	}
	if rowsErr != nil {
		return fmt.Errorf("ledger: sqlite range rows: %w", rowsErr)
	}
	for _, ts := range records {
		if !fn(ts) {
			break
		}
	}
	return nil
}
