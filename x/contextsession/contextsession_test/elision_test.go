package contextsession_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/x/contextsession"
)

func TestStubContent(t *testing.T) {
	short := []byte("small content")
	if got := contextsession.StubContent(short); !bytes.Equal(got, short) {
		t.Fatalf("StubContent(short) = %q, want unchanged", got)
	}

	exact := []byte(strings.Repeat("e", contextsession.StubContentBytes))
	if got := contextsession.StubContent(exact); !bytes.Equal(got, exact) {
		t.Fatalf("StubContent at exactly the cap changed content")
	}

	long := []byte(strings.Repeat("l", contextsession.StubContentBytes+100))
	got := contextsession.StubContent(long)
	if len(got) != contextsession.StubContentBytes {
		t.Fatalf("StubContent(long) len = %d, want %d", len(got), contextsession.StubContentBytes)
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
			content:    []byte(strings.Repeat("é", contextsession.StubContentBytes)),
			wantLength: contextsession.StubContentBytes - 1,
		},
		{
			name:       "ascii over the cap",
			content:    []byte(strings.Repeat("a", contextsession.StubContentBytes+1)),
			wantLength: contextsession.StubContentBytes,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextsession.StubContent(tt.content)
			if !utf8.Valid(got) {
				t.Fatalf("StubContent = %q, want valid UTF-8", got)
			}
			if !bytes.HasSuffix(got, []byte("[elided]")) {
				t.Fatalf("StubContent = %q, want the truncation marker at the end", got)
			}
			if len(got) != tt.wantLength {
				t.Fatalf("len(StubContent) = %d, want %d", len(got), tt.wantLength)
			}
			if len(got) > contextsession.StubContentBytes {
				t.Fatalf("len(StubContent) = %d, over the cap %d", len(got), contextsession.StubContentBytes)
			}
		})
	}
}

func TestNewPlannerNilArguments(t *testing.T) {
	store := newStore(t)

	if _, err := contextsession.NewPlanner(nil, nil); !errors.Is(err, contextsession.ErrNilStore) {
		t.Fatalf("err = %v, want ErrNilStore", err)
	}
	p, err := contextsession.NewPlanner(store, nil)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	if p == nil {
		t.Fatal("NewPlanner returned a nil Planner on success")
	}
}
