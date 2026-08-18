//go:build ledger_sqlite

package ledger

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestCompareAndSwapInsertSetsCreatedAndUpdatedAudit proves
// CompareAndSwap's insert branch stores CreatedBy, CreatedAt,
// UpdatedBy, and UpdatedAt from the inserted TaskState, read back
// through a raw column query.
func TestCompareAndSwapInsertSetsCreatedAndUpdatedAudit(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")

	createdAt := fixedSQLiteNow
	updatedAt := fixedSQLiteNow.Add(time.Minute)
	in := TaskState{
		Key:       "k1",
		Status:    StatusPending,
		Sequence:  1,
		CreatedBy: Actor("actor-insert"),
		CreatedAt: createdAt,
		UpdatedBy: Actor("actor-insert"),
		UpdatedAt: updatedAt,
	}
	if ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, in); err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}

	gotCreatedAt, gotUpdatedAt, gotCreatedBy, gotUpdatedBy := readAuditColumns(t, store.db, "k1")
	if gotCreatedBy != "actor-insert" || gotCreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("created_by/created_at = %q/%q, want %q/%q", gotCreatedBy, gotCreatedAt, "actor-insert", createdAt.Format(time.RFC3339Nano))
	}
	if gotUpdatedBy != "actor-insert" || gotUpdatedAt != updatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("updated_by/updated_at = %q/%q, want %q/%q", gotUpdatedBy, gotUpdatedAt, "actor-insert", updatedAt.Format(time.RFC3339Nano))
	}
}

// TestCompareAndSwapUpdateChangesUpdatedAuditOnly proves an update
// through CompareAndSwap changes updated_at/updated_by while leaving
// created_at/created_by byte-identical to the insert-time values.
func TestCompareAndSwapUpdateChangesUpdatedAuditOnly(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteStoreT(t, ":memory:")

	createdAt := fixedSQLiteNow
	inserted := TaskState{
		Key:       "k1",
		Status:    StatusPending,
		Sequence:  1,
		CreatedBy: Actor("actor-a"),
		CreatedAt: createdAt,
		UpdatedBy: Actor("actor-a"),
		UpdatedAt: createdAt,
	}
	if ok, err := store.CompareAndSwap(ctx, "k1", TaskState{}, inserted); err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}
	stored, found, err := store.Load(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}

	updatedAt := createdAt.Add(time.Hour)
	next := stored
	next.Sequence = 2
	next.UpdatedBy = Actor("actor-b")
	next.UpdatedAt = updatedAt
	if ok, err := store.CompareAndSwap(ctx, "k1", stored, next); err != nil || !ok {
		t.Fatalf("update: ok=%v err=%v", ok, err)
	}

	gotCreatedAt, gotUpdatedAt, gotCreatedBy, gotUpdatedBy := readAuditColumns(t, store.db, "k1")
	if gotCreatedBy != "actor-a" || gotCreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("created_by/created_at after update = %q/%q, want unchanged %q/%q", gotCreatedBy, gotCreatedAt, "actor-a", createdAt.Format(time.RFC3339Nano))
	}
	if gotUpdatedBy != "actor-b" || gotUpdatedAt != updatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("updated_by/updated_at after update = %q/%q, want %q/%q", gotUpdatedBy, gotUpdatedAt, "actor-b", updatedAt.Format(time.RFC3339Nano))
	}
}

// readAuditColumns reads the four audit columns for key through a raw
// query, bypassing scanTaskState so a test can assert the exact
// stored text independent of TaskState's own parse round trip.
func readAuditColumns(t *testing.T, db *sql.DB, key IdempotencyKey) (createdAt, updatedAt, createdBy, updatedBy string) {
	t.Helper()
	row := db.QueryRow("SELECT created_at, updated_at, created_by, updated_by FROM ledger_tasks WHERE key = ?", string(key))
	if err := row.Scan(&createdAt, &updatedAt, &createdBy, &updatedBy); err != nil {
		t.Fatalf("read audit columns for %q: %v", key, err)
	}
	return createdAt, updatedAt, createdBy, updatedBy
}
