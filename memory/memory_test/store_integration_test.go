package memory_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

// TestPutGetRefMatchesContextRef puts two distinct blobs, gets each
// back by ref, and proves each ref equals envelope.ContextRef of the
// original content. A third blob that pushes the store over budget
// evicts the oldest blob; a fourth blob larger than the whole budget
// is rejected outright and never resolves.
func TestPutGetRefMatchesContextRef(t *testing.T) {
	s, err := memory.New(10)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	blobA := []byte("aaaaa")
	blobB := []byte("bbbbb")
	refA, err := s.Put(blobA)
	if err != nil {
		t.Fatalf("Put(A) error = %v", err)
	}
	refB, err := s.Put(blobB)
	if err != nil {
		t.Fatalf("Put(B) error = %v", err)
	}
	if want := envelope.ContextRef(string(blobA)); refA != want {
		t.Fatalf("refA = %q, want %q", refA, want)
	}
	if want := envelope.ContextRef(string(blobB)); refB != want {
		t.Fatalf("refB = %q, want %q", refB, want)
	}
	gotA, err := s.Get(refA)
	if err != nil || string(gotA) != string(blobA) {
		t.Fatalf("Get(refA) = %q, %v, want %q, nil", gotA, err, blobA)
	}
	gotB, err := s.Get(refB)
	if err != nil || string(gotB) != string(blobB) {
		t.Fatalf("Get(refB) = %q, %v, want %q, nil", gotB, err, blobB)
	}

	// A and B together fill the 10-byte budget. A third 5-byte blob
	// forces one eviction: A, the oldest, must go; B must survive.
	blobC := []byte("ccccc")
	refC, err := s.Put(blobC)
	if err != nil {
		t.Fatalf("Put(C) error = %v", err)
	}
	if _, err := s.Get(refA); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("Get(refA) after eviction error = %v, want %v", err, memory.ErrUnknownRef)
	}
	if _, err := s.Get(refB); err != nil {
		t.Fatalf("Get(refB) after eviction error = %v, want nil", err)
	}
	if _, err := s.Get(refC); err != nil {
		t.Fatalf("Get(refC) error = %v, want nil", err)
	}

	// A blob larger than the whole budget is rejected outright.
	oversize := []byte("this blob is far larger than the ten byte budget")
	refOversize, err := s.Put(oversize)
	if !errors.Is(err, memory.ErrBudgetExceeded) {
		t.Fatalf("Put(oversize) error = %v, want %v", err, memory.ErrBudgetExceeded)
	}
	wantRef := envelope.ContextRef(string(oversize))
	if _, err := s.Get(wantRef); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("Get(oversize ref) error = %v, want %v", err, memory.ErrUnknownRef)
	}
	if refOversize != "" {
		t.Fatalf("Put(oversize) ref = %q, want empty", refOversize)
	}
}

// TestPutRefreshesInsertionOrder puts A, then B, then re-puts A.
// The re-put must refresh A's insertion order to most-recent, so a
// following Put that forces exactly one eviction evicts B, not A.
func TestPutRefreshesInsertionOrder(t *testing.T) {
	s, err := memory.New(12)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	blobA := []byte("aaaaa")
	blobB := []byte("bbbbb")
	refA, err := s.Put(blobA)
	if err != nil {
		t.Fatalf("Put(A) error = %v", err)
	}
	refB, err := s.Put(blobB)
	if err != nil {
		t.Fatalf("Put(B) error = %v", err)
	}
	// Re-put A: same ref, refreshes insertion order to most-recent.
	refA2, err := s.Put(blobA)
	if err != nil {
		t.Fatalf("re-Put(A) error = %v", err)
	}
	if refA2 != refA {
		t.Fatalf("re-Put(A) ref = %q, want %q", refA2, refA)
	}

	// A, B fill 10 of the 12-byte budget. C is sized to force exactly
	// one eviction: B, now the oldest, must go; A and C must survive.
	blobC := []byte("ccccc")
	refC, err := s.Put(blobC)
	if err != nil {
		t.Fatalf("Put(C) error = %v", err)
	}
	if _, err := s.Get(refB); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("Get(refB) after refresh-driven eviction error = %v, want %v", err, memory.ErrUnknownRef)
	}
	if _, err := s.Get(refA); err != nil {
		t.Fatalf("Get(refA) after refresh-driven eviction error = %v, want nil", err)
	}
	if _, err := s.Get(refC); err != nil {
		t.Fatalf("Get(refC) error = %v, want nil", err)
	}
}

// TestPutMultiBlobEviction fills the budget with three small blobs,
// then puts a fourth sized to force the eviction of two of the
// three, not just the oldest one.
func TestPutMultiBlobEviction(t *testing.T) {
	s, err := memory.New(9)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	refA, err := s.Put([]byte("aaa"))
	if err != nil {
		t.Fatalf("Put(A) error = %v", err)
	}
	refB, err := s.Put([]byte("bbb"))
	if err != nil {
		t.Fatalf("Put(B) error = %v", err)
	}
	refC, err := s.Put([]byte("ccc"))
	if err != nil {
		t.Fatalf("Put(C) error = %v", err)
	}
	// A, B, C fill the 9-byte budget. A 6-byte D forces the eviction
	// of both A and B; only C survives alongside D.
	refD, err := s.Put([]byte("dddddd"))
	if err != nil {
		t.Fatalf("Put(D) error = %v", err)
	}
	if _, err := s.Get(refA); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("Get(refA) after multi-eviction error = %v, want %v", err, memory.ErrUnknownRef)
	}
	if _, err := s.Get(refB); !errors.Is(err, memory.ErrUnknownRef) {
		t.Fatalf("Get(refB) after multi-eviction error = %v, want %v", err, memory.ErrUnknownRef)
	}
	if _, err := s.Get(refC); err != nil {
		t.Fatalf("Get(refC) after multi-eviction error = %v, want nil", err)
	}
	if _, err := s.Get(refD); err != nil {
		t.Fatalf("Get(refD) error = %v, want nil", err)
	}
}
