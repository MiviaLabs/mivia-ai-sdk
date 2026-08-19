package diff_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/diff"
)

func TestUnifiedIdenticalInput(t *testing.T) {
	got, err := diff.Unified("f.txt", []byte("one\ntwo\n"), []byte("one\ntwo\n"), 0)
	if err != nil {
		t.Fatalf("Unified: %v", err)
	}
	if got != "" {
		t.Errorf("Unified(identical) = %q, want empty string", got)
	}
}

func TestUnifiedSimpleChanges(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "appended line",
			a:    "one\ntwo\nthree\n",
			b:    "one\ntwo\nthree\nfour\n",
			want: "--- f.txt\n+++ f.txt\n@@ -1,3 +1,4 @@\n one\n two\n three\n+four\n",
		},
		{
			name: "removed line",
			a:    "one\ntwo\nthree\n",
			b:    "one\nthree\n",
			want: "--- f.txt\n+++ f.txt\n@@ -1,3 +1,2 @@\n one\n-two\n three\n",
		},
		{
			name: "changed line in middle",
			a:    "one\ntwo\nthree\n",
			b:    "one\nTWO\nthree\n",
			want: "--- f.txt\n+++ f.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := diff.Unified("f.txt", []byte(tt.a), []byte(tt.b), 0)
			if err != nil {
				t.Fatalf("Unified: %v", err)
			}
			if got != tt.want {
				t.Errorf("Unified() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnifiedNoTrailingNewline(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "a lacks trailing newline",
			a:    "one\ntwo",
			b:    "one\ntwo\n",
			want: "--- f.txt\n+++ f.txt\n@@ -1,2 +1,2 @@\n one\n-two\n\\ No newline at end of file\n+two\n",
		},
		{
			name: "b lacks trailing newline",
			a:    "one\ntwo\n",
			b:    "one\ntwo",
			want: "--- f.txt\n+++ f.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+two\n\\ No newline at end of file\n",
		},
		{
			name: "both lack trailing newline with a content change",
			a:    "one\ntwo",
			b:    "one\nTWO",
			want: "--- f.txt\n+++ f.txt\n@@ -1,2 +1,2 @@\n one\n-two\n\\ No newline at end of file\n+TWO\n\\ No newline at end of file\n",
		},
		{
			name: "trailing newline differs and last line content also differs",
			a:    "one\ntwo",
			b:    "one\nTWO\n",
			want: "--- f.txt\n+++ f.txt\n@@ -1,2 +1,2 @@\n one\n-two\n\\ No newline at end of file\n+TWO\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := diff.Unified("f.txt", []byte(tt.a), []byte(tt.b), 0)
			if err != nil {
				t.Fatalf("Unified: %v", err)
			}
			if got != tt.want {
				t.Errorf("Unified() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnifiedBoundCases(t *testing.T) {
	a := []byte("one\ntwo\nthree\n")
	b := []byte("one\ntwo\nthree\nfour\n")

	got, err := diff.Unified("f.txt", a, b, 7)
	if err != nil {
		t.Fatalf("Unified(maxLines=7): %v", err)
	}
	if got == "" {
		t.Error("Unified(maxLines=7) = empty, want a fitting diff")
	}

	_, err = diff.Unified("f.txt", a, b, 6)
	if !errors.Is(err, diff.ErrTooLarge) {
		t.Errorf("Unified(maxLines=6) error = %v, want ErrTooLarge", err)
	}

	got, err = diff.Unified("f.txt", a, b, 0)
	if err != nil {
		t.Fatalf("Unified(maxLines=0): %v", err)
	}
	if got == "" {
		t.Error("Unified(maxLines=0) = empty, want a diff")
	}

	got, err = diff.Unified("f.txt", a, b, -1)
	if err != nil {
		t.Fatalf("Unified(maxLines=-1): %v", err)
	}
	if got == "" {
		t.Error("Unified(maxLines=-1) = empty, want a diff")
	}
}

func TestUnifiedEmptyInputCases(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		got, err := diff.Unified("f.txt", []byte(""), []byte(""), 0)
		if err != nil {
			t.Fatalf("Unified: %v", err)
		}
		if got != "" {
			t.Errorf("Unified(both empty) = %q, want empty", got)
		}
	})

	t.Run("a empty, b many lines", func(t *testing.T) {
		want := "--- f.txt\n+++ f.txt\n@@ -0,0 +1,2 @@\n+x\n+y\n"
		got, err := diff.Unified("f.txt", []byte(""), []byte("x\ny\n"), 0)
		if err != nil {
			t.Fatalf("Unified: %v", err)
		}
		if got != want {
			t.Errorf("Unified() = %q, want %q", got, want)
		}
	})

	t.Run("a single line, b empty", func(t *testing.T) {
		want := "--- f.txt\n+++ f.txt\n@@ -1 +0,0 @@\n-x\n"
		got, err := diff.Unified("f.txt", []byte("x\n"), []byte(""), 0)
		if err != nil {
			t.Fatalf("Unified: %v", err)
		}
		if got != want {
			t.Errorf("Unified() = %q, want %q", got, want)
		}
	})
}

func TestUnifiedHunkMerging(t *testing.T) {
	// Two changes five lines apart merge into one hunk (within 2*N = 6).
	a := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n"
	b := "L1\nl2\nl3\nl4\nl5\nl6\nL7\nl8\n"
	got, err := diff.Unified("f.txt", []byte(a), []byte(b), 0)
	if err != nil {
		t.Fatalf("Unified: %v", err)
	}
	count := 0
	for i := 0; i+3 <= len(got); i++ {
		if got[i:i+3] == "@@ " {
			count++
		}
	}
	if count != 1 {
		t.Errorf("hunk count = %d, want 1 (changes should merge)", count)
	}
}

// FuzzUnified feeds arbitrary byte pairs to Unified. It must never
// panic, and identical input must always yield an empty diff, since
// that is the one invariant Unified documents unconditionally.
// Run: go test -fuzz=FuzzUnified ./diff/diff_test/
func FuzzUnified(f *testing.F) {
	seeds := []struct {
		a, b string
	}{
		{"one\ntwo\nthree\n", "one\ntwo\nthree\nfour\n"},
		{"one\ntwo\nthree\n", "one\nthree\n"},
		{"one\ntwo\nthree\n", "one\nTWO\nthree\n"},
		{"one\ntwo", "one\ntwo\n"},
		{"", ""},
		{"", "x\ny\n"},
		{"x\n", ""},
		{"same\n", "same\n"},
	}
	for _, s := range seeds {
		f.Add([]byte(s.a), []byte(s.b))
	}
	f.Fuzz(func(t *testing.T, a, b []byte) {
		got, err := diff.Unified("f.txt", a, b, 0)
		if bytes.Equal(a, b) {
			if err != nil {
				t.Fatalf("Unified(identical) error = %v, want nil", err)
			}
			if got != "" {
				t.Fatalf("Unified(identical) = %q, want empty string", got)
			}
			return
		}
		if err != nil && !errors.Is(err, diff.ErrTooLarge) {
			t.Fatalf("Unified() error = %v, want nil or ErrTooLarge", err)
		}
	})
}

func TestUnifiedHunksDoNotMergeWhenFar(t *testing.T) {
	a := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nl20\n"
	b := "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nL19\nl20\n"
	got, err := diff.Unified("f.txt", []byte(a), []byte(b), 0)
	if err != nil {
		t.Fatalf("Unified: %v", err)
	}
	count := 0
	for i := 0; i+3 <= len(got); i++ {
		if got[i:i+3] == "@@ " {
			count++
		}
	}
	if count != 2 {
		t.Errorf("hunk count = %d, want 2 (changes should not merge)", count)
	}
}
