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

// createLedgerTasksTable is the single table SQLiteStore needs,
// created idempotently on every open.
const createLedgerTasksTable = `
CREATE TABLE IF NOT EXISTS ledger_tasks (
    key         TEXT PRIMARY KEY,
    status      TEXT NOT NULL,
    sequence    INTEGER NOT NULL,
    owner       TEXT NOT NULL DEFAULT '',
    fence       INTEGER NOT NULL DEFAULT 0,
    lease_until TEXT NOT NULL DEFAULT '',
    needs       TEXT NOT NULL DEFAULT '[]',
    blocked_by  TEXT NOT NULL DEFAULT '',
    task        BLOB,
    rev         INTEGER NOT NULL DEFAULT 0
)`

// selectLedgerTask reads every column Load and Range need.
const selectLedgerTaskColumns = "key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev"

// insertLedgerTask maps CompareAndSwap's insert-if-absent branch: a
// zero-value old against an absent key.
const insertLedgerTask = `
INSERT INTO ledger_tasks
    (key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
ON CONFLICT(key) DO NOTHING`

// updateLedgerTask maps CompareAndSwap's conditional-update branch,
// carrying every compare field in its WHERE clause and bumping rev
// inline.
const updateLedgerTask = `
UPDATE ledger_tasks
SET status = ?, sequence = ?, owner = ?, fence = ?, lease_until = ?,
    needs = ?, blocked_by = ?, task = ?, rev = rev + 1
WHERE key = ? AND sequence = ? AND status = ? AND fence = ? AND rev = ?`

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
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ledger: ping sqlite store: %w", err)
	}
	return &SQLiteStore{db: db}, nil
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
	)
	if err := row.Scan(&key, &status, &sequence, &owner, &fence, &leaseUntil, &needsJSON, &blockedBy, &taskBytes, &rev); err != nil {
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
	}, nil
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
	if old.Sequence == 0 && old.Status == "" && old.Fence == 0 && old.Rev == 0 {
		res, err := s.db.ExecContext(ctx, insertLedgerTask,
			string(key), string(new.Status), uint64(new.Sequence), string(new.Owner),
			uint64(new.Fence), leaseUntil, string(needsJSON), string(new.BlockedBy), taskBytes)
		if err != nil {
			return false, fmt.Errorf("ledger: sqlite insert: %w", err)
		}
		return rowsAffectedOne(res)
	}
	res, err := s.db.ExecContext(ctx, updateLedgerTask,
		string(new.Status), uint64(new.Sequence), string(new.Owner), uint64(new.Fence),
		leaseUntil, string(needsJSON), string(new.BlockedBy), taskBytes,
		string(key), uint64(old.Sequence), string(old.Status), uint64(old.Fence), uint64(old.Rev))
	if err != nil {
		return false, fmt.Errorf("ledger: sqlite update: %w", err)
	}
	return rowsAffectedOne(res)
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
