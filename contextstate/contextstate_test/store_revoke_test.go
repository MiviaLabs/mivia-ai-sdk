package contextstate_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// TestGetDeniesRevoked kills a mutation that drops the Revoked check,
// returns the old Data, or returns a non-zero record on this error.
func TestGetDeniesRevoked(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("revoked-bytes"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := store.Get(record.Ref)
	if !errors.Is(err, contextstate.ErrPayloadRevoked) {
		t.Fatalf("Get on revoked record: err = %v, want ErrPayloadRevoked", err)
	}
	if !payloadEqual(got, contextstate.PayloadRecord{}) {
		t.Fatalf("Get on revoked record returned %+v, want the zero value", got)
	}
}

// TestGetNonRevokedUnchanged kills a mutation that revokes by default
// or checks the wrong field.
func TestGetNonRevokedUnchanged(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("live-bytes"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(record.Ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !payloadEqual(got, record) {
		t.Fatalf("Get = %+v, want %+v", got, record)
	}
}

// TestStatusRevoked kills a mutation that turns Status into a second
// Get, or one that wraps an error for a revoked-but-found ref.
func TestStatusRevoked(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("status-revoked"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	status, err := store.Status(record.Ref)
	if err != nil {
		t.Fatalf("Status on revoked record: %v, want nil error", err)
	}
	if status.Data != nil {
		t.Fatalf("Status.Data = %v, want nil", status.Data)
	}
	if !status.Revoked {
		t.Fatal("Status.Revoked = false, want true")
	}
	if status.Ref != record.Ref || status.Retention != record.Retention {
		t.Fatalf("Status metadata = %+v, want Ref and Retention to match the stored record", status)
	}
}

// TestStatusNonRevoked kills a mutation that leaks Data through
// Status.
func TestStatusNonRevoked(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("status-live"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	status, err := store.Status(record.Ref)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Data != nil {
		t.Fatalf("Status.Data = %v, want nil", status.Data)
	}
	if status.Revoked {
		t.Fatal("Status.Revoked = true, want false")
	}
}

// TestStatusUnknownRef kills a mutation that returns a false success
// for an unstored ref.
func TestStatusUnknownRef(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	unknown, err := contextstate.NewContentRef("fixture-ns", "workspace-a", "session-a", "subject-a", []byte("never-stored"))
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	if _, err := store.Status(unknown); !errors.Is(err, contextstate.ErrPayloadNotFound) {
		t.Fatalf("Status of unknown ref: %v, want ErrPayloadNotFound", err)
	}
}

// TestRevokeMarksStoredRecord kills a mutation that no-ops Revoke.
func TestRevokeMarksStoredRecord(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("mark-me"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := store.Get(record.Ref); !errors.Is(err, contextstate.ErrPayloadRevoked) {
		t.Fatalf("Get after Revoke: %v, want ErrPayloadRevoked", err)
	}
	status, err := store.Status(record.Ref)
	if err != nil {
		t.Fatalf("Status after Revoke: %v", err)
	}
	if !status.Revoked {
		t.Fatal("Status after Revoke reports Revoked == false")
	}
}

// TestRevokeTwiceIsNoOp kills a mutation that errors, or un-revokes,
// on the second call.
func TestRevokeTwiceIsNoOp(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	record := fixturePayload(t, "session-a", []byte("double-revoke"))
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("second Revoke: %v, want nil", err)
	}
	status, err := store.Status(record.Ref)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Revoked {
		t.Fatal("second Revoke un-revoked the record")
	}
}

// TestRevokeUnknownRef kills a mutation that treats an unknown ref as
// success or the wrong sentinel.
func TestRevokeUnknownRef(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	unknown, err := contextstate.NewContentRef("fixture-ns", "workspace-a", "session-a", "subject-a", []byte("never-stored"))
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	if err := store.Revoke(unknown); !errors.Is(err, contextstate.ErrPayloadNotFound) {
		t.Fatalf("Revoke of unknown ref: %v, want ErrPayloadNotFound", err)
	}
}

// TestPutAfterRevokeSameRefIsNoOp kills a mutation that lets a
// re-Put clear Revoked or overwrite a revoked record's fields.
func TestPutAfterRevokeSameRefIsNoOp(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	data := []byte("tamper-guard")
	record := fixturePayload(t, "session-a", data)
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(record.Ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	// A re-Put under the same ref, digest-matching data, but a
	// different Retention and an explicit Revoked: false, must not
	// change the stored record.
	reput := record
	reput.Retention = contextstate.RetentionCompliance
	reput.Revoked = false
	if err := store.Put(reput); err != nil {
		t.Fatalf("Put after Revoke: %v, want nil (no-op)", err)
	}
	if _, err := store.Get(record.Ref); !errors.Is(err, contextstate.ErrPayloadRevoked) {
		t.Fatalf("Get after re-Put: %v, want ErrPayloadRevoked", err)
	}
	status, err := store.Status(record.Ref)
	if err != nil {
		t.Fatalf("Status after re-Put: %v", err)
	}
	if !status.Revoked {
		t.Fatal("re-Put cleared Revoked")
	}
	if status.Retention != record.Retention {
		t.Fatalf("re-Put changed Retention: got %q, want the original %q", status.Retention, record.Retention)
	}
}

// TestPutAfterRevokeDifferentRefStillWrites kills a mutation that
// makes Put's guard key on the wrong field.
func TestPutAfterRevokeDifferentRefStillWrites(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	revoked := fixturePayload(t, "session-a", []byte("revoked-content"))
	if err := store.Put(revoked); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Revoke(revoked.Ref); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	other := fixturePayload(t, "session-a", []byte("distinct-content"))
	if err := store.Put(other); err != nil {
		t.Fatalf("Put of a distinct ref: %v, want nil", err)
	}
	got, err := store.Get(other.Ref)
	if err != nil {
		t.Fatalf("Get of the distinct ref: %v", err)
	}
	if !bytes.Equal(got.Data, other.Data) {
		t.Fatal("Put of a distinct ref did not write normally")
	}
}

// TestStoreRevokeConcurrency runs Revoke, Put, Get, and Status
// concurrently on a shared ref and on distinct refs, under -race.
// Kills a lock-scope regression and a mutation that races the guard
// check against the write in Put.
func TestStoreRevokeConcurrency(t *testing.T) {
	store := newStore(t, contextstate.Limits{})
	shared := fixturePayload(t, "shared-session", []byte("shared-payload"))
	if err := store.Put(shared); err != nil {
		t.Fatalf("seed Put: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			own := fixturePayload(t, "own-session", []byte("own-payload"))
			if err := store.Put(own); err != nil {
				t.Errorf("Put(own %d): %v", idx, err)
			}
			if err := store.Revoke(own.Ref); err != nil {
				t.Errorf("Revoke(own %d): %v", idx, err)
			}
			if _, err := store.Status(own.Ref); err != nil {
				t.Errorf("Status(own %d): %v", idx, err)
			}

			// Every goroutine races Revoke, Put, Get, and Status against
			// the same shared ref.
			_ = store.Revoke(shared.Ref)
			_ = store.Put(shared)
			if _, err := store.Status(shared.Ref); err != nil {
				t.Errorf("Status(shared %d): %v", idx, err)
			}
			_, getErr := store.Get(shared.Ref)
			if getErr != nil && !errors.Is(getErr, contextstate.ErrPayloadRevoked) {
				t.Errorf("Get(shared %d): %v, want nil or ErrPayloadRevoked", idx, getErr)
			}
		}(i)
	}
	wg.Wait()
	if t.Failed() {
		t.Fatal("concurrent revoke/put/get/status failed; see logs")
	}
}
