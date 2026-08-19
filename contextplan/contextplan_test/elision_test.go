package contextplan_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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

// TestStubContentRuneSafe checks the rune-safe cut. A cut boundary
// inside a multi-byte rune leaves valid UTF-8 and keeps the marker.
// Pure ASCII content over the cap still fills the cap exactly.
func TestStubContentRuneSafe(t *testing.T) {
	tests := []struct {
		name       string
		content    []byte
		wantLength int
	}{
		{
			name:       "cut inside a rune",
			content:    []byte(strings.Repeat("é", contextplan.StubContentBytes)),
			wantLength: contextplan.StubContentBytes - 1,
		},
		{
			name:       "ascii over the cap",
			content:    []byte(strings.Repeat("a", contextplan.StubContentBytes+1)),
			wantLength: contextplan.StubContentBytes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextplan.StubContent(tt.content)
			if !utf8.Valid(got) {
				t.Fatalf("StubContent = %q, want valid UTF-8", got)
			}
			if !bytes.HasSuffix(got, []byte("[elided]")) {
				t.Fatalf("StubContent = %q, want the truncation marker at the end", got)
			}
			if len(got) != tt.wantLength {
				t.Fatalf("len(StubContent) = %d, want %d", len(got), tt.wantLength)
			}
			if len(got) > contextplan.StubContentBytes {
				t.Fatalf("len(StubContent) = %d, over the cap %d", len(got), contextplan.StubContentBytes)
			}
		})
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
