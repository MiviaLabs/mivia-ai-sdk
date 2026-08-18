//go:build ledger_sqlite

package ledger

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
    rev         INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    updated_by  TEXT NOT NULL DEFAULT ''
)`

// selectLedgerTaskColumns reads every column Load and Range need.
const selectLedgerTaskColumns = "key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev, created_at, updated_at, created_by, updated_by"

// insertLedgerTask maps CompareAndSwap's insert-if-absent branch: a
// zero-value old against an absent key.
const insertLedgerTask = `
INSERT INTO ledger_tasks
    (key, status, sequence, owner, fence, lease_until, needs, blocked_by, task, rev, created_at, updated_at, created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)
ON CONFLICT(key) DO NOTHING`

// updateLedgerTask maps CompareAndSwap's conditional-update branch,
// carrying every compare field in its WHERE clause and bumping rev
// inline.
const updateLedgerTask = `
UPDATE ledger_tasks
SET status = ?, sequence = ?, owner = ?, fence = ?, lease_until = ?,
    needs = ?, blocked_by = ?, task = ?, rev = rev + 1,
    updated_at = ?, updated_by = ?
WHERE key = ? AND sequence = ? AND status = ? AND fence = ? AND rev = ?`

// auditColumns lists the four audit columns migrateAuditColumns adds
// to a pre-this-phase ledger_tasks table, in a fixed order.
var auditColumns = []string{"created_at", "updated_at", "created_by", "updated_by"}
