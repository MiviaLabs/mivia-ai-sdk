//go:build ledger_sqlite

package ledger

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

// preAuditLedgerTasksTable is the exact pre-this-phase ledger_tasks
// DDL, with no audit columns, used to seed a legacy database file for
// a migration test.
const preAuditLedgerTasksTable = `
CREATE TABLE ledger_tasks (
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

// openRawSQLite opens a *sql.DB against path with the same driver and
// pragma DSN SQLiteStore itself uses, bypassing NewSQLiteStore so a
// test can build a schema NewSQLiteStore's own CREATE TABLE never
// produces.
func openRawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open(sqliteDriverName, sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// tableInfoColumns returns every column name PRAGMA table_info reports
// for ledger_tasks.
func tableInfoColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(ledger_tasks)")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			name, typ                string
			dfltValue                any
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &primaryKey); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	return cols
}

// TestNewSQLiteStoreMigratesPreexistingSchema proves NewSQLiteStore
// against a file whose ledger_tasks table predates this phase adds
// all four audit columns, preserves the pre-existing row's other
// columns, and backfills each audit column as the empty string for
// that row.
func TestNewSQLiteStoreMigratesPreexistingSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	seed := openRawSQLite(t, path)
	if _, err := seed.Exec(preAuditLedgerTasksTable); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := seed.Exec(
		"INSERT INTO ledger_tasks (key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"k1", string(StatusPending), 1, "", 0, "", "[]", "", []byte("null"), 0,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store := newSQLiteStoreT(t, path)

	ts, found, err := store.Load(ctx, "k1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatalf("Load: want found")
	}
	if ts.Status != StatusPending || ts.Sequence != 1 {
		t.Fatalf("migrated row = %+v, want Status=%q Sequence=1", ts, StatusPending)
	}

	cols := tableInfoColumns(t, store.db)
	for _, name := range auditColumns {
		if !cols[name] {
			t.Fatalf("table_info after migration lacks column %q", name)
		}
	}

	createdAt, updatedAt, createdBy, updatedBy := readAuditColumns(t, store.db, "k1")
	if createdAt != "" || updatedAt != "" || createdBy != "" || updatedBy != "" {
		t.Fatalf("migrated row audit columns = %q/%q/%q/%q, want all empty", createdAt, updatedAt, createdBy, updatedBy)
	}

	if !ts.CreatedAt.IsZero() || !ts.UpdatedAt.IsZero() || ts.CreatedBy != "" || ts.UpdatedBy != "" {
		t.Fatalf("migrated row audit fields via Load = %+v, want all zero", ts)
	}
}

// TestNewSQLiteStoreMigrationIsIdempotent proves a second
// NewSQLiteStore open against an already-migrated file succeeds and
// leaves the schema unchanged: migrateAuditColumns adds no duplicate
// column and returns no error the second time.
func TestNewSQLiteStoreMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")

	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("first NewSQLiteStore: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("second NewSQLiteStore: %v", err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second store: %v", err)
		}
	}()

	cols := tableInfoColumns(t, second.db)
	for _, name := range auditColumns {
		if !cols[name] {
			t.Fatalf("table_info after second open lacks column %q", name)
		}
	}
}

// TestNewSQLiteStoreMigratesPartialSchema proves NewSQLiteStore
// against a file whose ledger_tasks table already carries
// created_at/created_by (an asymmetric, partially migrated schema)
// adds only the still-missing updated_at/updated_by columns and
// leaves the pre-existing created_at/created_by values untouched.
func TestNewSQLiteStoreMigratesPartialSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")

	seed := openRawSQLite(t, path)
	if _, err := seed.Exec(preAuditLedgerTasksTable); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := seed.Exec("ALTER TABLE ledger_tasks ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("add created_at: %v", err)
	}
	if _, err := seed.Exec("ALTER TABLE ledger_tasks ADD COLUMN created_by TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("add created_by: %v", err)
	}
	if _, err := seed.Exec(
		"INSERT INTO ledger_tasks (key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"k1", string(StatusPending), 1, "", 0, "", "[]", "", []byte("null"), 0, "2024-01-01T00:00:00Z", "legacy-actor",
	); err != nil {
		t.Fatalf("seed partial row: %v", err)
	}

	beforeCreatedAt, beforeCreatedBy := "", ""
	if err := seed.QueryRow("SELECT created_at, created_by FROM ledger_tasks WHERE key = ?", "k1").Scan(&beforeCreatedAt, &beforeCreatedBy); err != nil {
		t.Fatalf("read pre-migration created_at/created_by: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	store := newSQLiteStoreT(t, path)

	cols := tableInfoColumns(t, store.db)
	for _, name := range auditColumns {
		if !cols[name] {
			t.Fatalf("table_info after migration lacks column %q", name)
		}
	}

	createdAt, updatedAt, createdBy, updatedBy := readAuditColumns(t, store.db, "k1")
	if createdAt != beforeCreatedAt || createdBy != beforeCreatedBy {
		t.Fatalf("created_at/created_by = %q/%q, want unchanged %q/%q", createdAt, createdBy, beforeCreatedAt, beforeCreatedBy)
	}
	if updatedAt != "" || updatedBy != "" {
		t.Fatalf("updated_at/updated_by = %q/%q, want both empty", updatedAt, updatedBy)
	}
}

// TestNewSQLiteStoreMigrationIsConcurrencySafe proves two concurrent
// NewSQLiteStore opens against the same pre-audit-schema file both
// succeed, because migrateAuditColumns's BEGIN IMMEDIATE transaction
// takes the write lock before its PRAGMA table_info check runs, so
// the two opens cannot both observe a column absent and both add it.
// Both resulting schemas list each audit column exactly once.
func TestNewSQLiteStoreMigrationIsConcurrencySafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")

	seed := openRawSQLite(t, path)
	if _, err := seed.Exec(preAuditLedgerTasksTable); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	var (
		wg     sync.WaitGroup
		stores [2]*SQLiteStore
		errs   [2]error
	)
	wg.Add(2)
	for i := range 2 {
		go func(i int) {
			defer wg.Done()
			stores[i], errs[i] = NewSQLiteStore(path)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("NewSQLiteStore goroutine %d: %v", i, err)
		}
	}
	defer func() {
		for i, store := range stores {
			if err := store.Close(); err != nil {
				t.Errorf("close store %d: %v", i, err)
			}
		}
	}()

	for i, store := range stores {
		counts := columnCounts(t, store.db)
		for _, name := range auditColumns {
			if counts[name] != 1 {
				t.Fatalf("store %d: column %q appears %d times, want 1", i, name, counts[name])
			}
		}
	}
}

// columnCounts returns, for each column PRAGMA table_info(ledger_tasks)
// reports, the number of times its name appears in that output. A
// duplicated column from a torn migration would report a count above
// one for its name.
func columnCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(ledger_tasks)")
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var (
			cid, notNull, primaryKey int
			name, typ                string
			dfltValue                any
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &primaryKey); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		counts[name]++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	return counts
}
