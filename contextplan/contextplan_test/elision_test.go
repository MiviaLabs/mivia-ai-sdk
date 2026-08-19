package contextplan_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
)

func TestStubContent(t *testing.T) {
	short := []byte("small content")
	if got := contextplan.StubContent(short); !bytes.Equal(got, short) {
		t.Fatalf("StubContent(short) = %q, want unchanged", got)
	}

	exact := []byte(strings.Repeat("e", contextplan.StubContentBytes))
	if got := contextplan.StubContent(exact); !bytes.Equal(got, exact) {
		t.Fatalf("StubContent at exactly the cap changed content")
	}

	long := []byte(strings.Repeat("l", contextplan.StubContentBytes+100))
	got := contextplan.StubContent(long)
	if len(got) != contextplan.StubContentBytes {
		t.Fatalf("StubContent(long) len = %d, want %d", len(got), contextplan.StubContentBytes)
	}
	if !bytes.Contains(got, []byte("[elided]")) {
		t.Fatalf("StubContent(long) = %q, want a truncation marker", got)
	}
}

func TestNewPlannerNilArguments(t *testing.T) {
	store := newStore(t)
	cache := newCache(t)

	if _, err := contextplan.NewPlanner(nil, cache); !errors.Is(err, contextplan.ErrNilStore) {
		t.Fatalf("err = %v, want ErrNilStore", err)
	}
	if _, err := contextplan.NewPlanner(store, nil); !errors.Is(err, contextplan.ErrNilCache) {
		t.Fatalf("err = %v, want ErrNilCache", err)
	}
	p, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	if p == nil {
		t.Fatal("NewPlanner returned a nil Planner on success")
	}
}
