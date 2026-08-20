//go:build ledger_sqlite

package ledger_test

import (
	"context"
	"testing"
)

// TestFenceMonotonicAcrossAdmitSQLiteStore runs the fence
// carry-forward contract against a file-backed SQLiteStore, so both
// shipped Store implementations hold it.
// Run under go test -tags ledger_sqlite ./ledger/...
func TestFenceMonotonicAcrossAdmitSQLiteStore(t *testing.T) {
	assertFenceMonotonicAcrossAdmit(t, context.Background(), sqliteLedgerT(t), "k1")
}
