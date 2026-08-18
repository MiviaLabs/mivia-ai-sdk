package memory_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int
		wantErr  error
	}{
		{"positive budget", 1024, nil},
		{"zero budget", 0, memory.ErrNoBudget},
		{"negative budget", -1, memory.ErrNoBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := memory.New(tt.maxBytes)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("New(%d) error = %v, want %v", tt.maxBytes, err, tt.wantErr)
				}
				if s != nil {
					t.Fatalf("New(%d) store = %v, want nil", tt.maxBytes, s)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%d) error = %v, want nil", tt.maxBytes, err)
			}
			if s == nil {
				t.Fatalf("New(%d) store = nil, want non-nil", tt.maxBytes)
			}
		})
	}
}

func TestPut(t *testing.T) {
	t.Run("blob under budget", func(t *testing.T) {
		s, err := memory.New(1024)
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ref, err := s.Put([]byte("hello"))
		if err != nil {
			t.Fatalf("Put error = %v, want nil", err)
		}
		if ref == "" {
			t.Fatalf("Put ref = %q, want non-empty", ref)
		}
	})

	t.Run("blob exactly at budget", func(t *testing.T) {
		s, err := memory.New(5)
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ref, err := s.Put([]byte("hello"))
		if err != nil {
			t.Fatalf("Put error = %v, want nil", err)
		}
		if ref == "" {
			t.Fatalf("Put ref = %q, want non-empty", ref)
		}
		got, err := s.Get(ref)
		if err != nil {
			t.Fatalf("Get error = %v, want nil", err)
		}
		if string(got) != "hello" {
			t.Fatalf("Get = %q, want %q", got, "hello")
		}
	})

	t.Run("blob over budget", func(t *testing.T) {
		s, err := memory.New(4)
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ref, err := s.Put([]byte("hello"))
		if !errors.Is(err, memory.ErrBudgetExceeded) {
			t.Fatalf("Put error = %v, want %v", err, memory.ErrBudgetExceeded)
		}
		if ref != "" {
			t.Fatalf("Put ref = %q, want empty", ref)
		}
		// A prior successful Put must survive a later rejected Put:
		// the store stays unchanged on rejection.
		earlier, err := s.Put([]byte("ok"))
		if err != nil {
			t.Fatalf("Put(ok) error = %v, want nil", err)
		}
		if _, err := s.Get(earlier); err != nil {
			t.Fatalf("Get(earlier) error = %v, want nil", err)
		}
	})

	t.Run("same content twice returns same ref", func(t *testing.T) {
		s, err := memory.New(1024)
		if err != nil {
			t.Fatalf("New error = %v", err)
		}
		ref1, err := s.Put([]byte("repeat me"))
		if err != nil {
			t.Fatalf("first Put error = %v", err)
		}
		ref2, err := s.Put([]byte("repeat me"))
		if err != nil {
			t.Fatalf("second Put error = %v", err)
		}
		if ref1 != ref2 {
			t.Fatalf("ref1 = %q, ref2 = %q, want equal", ref1, ref2)
		}
	})
}

// TestPutCopiesInput proves Put does not alias the caller's slice: a
// mutation after Put must not reach the stored blob.
func TestPutCopiesInput(t *testing.T) {
	s, err := memory.New(1024)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	content := []byte("original")
	ref, err := s.Put(content)
	if err != nil {
		t.Fatalf("Put error = %v", err)
	}
	content[0] = 'X'
	got, err := s.Get(ref)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("Get = %q, want %q; Put must copy its input", got, "original")
	}
}

func TestGet(t *testing.T) {
	s, err := memory.New(1024)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	ref, err := s.Put([]byte("known content"))
	if err != nil {
		t.Fatalf("Put error = %v", err)
	}

	t.Run("known ref", func(t *testing.T) {
		got, err := s.Get(ref)
		if err != nil {
			t.Fatalf("Get error = %v, want nil", err)
		}
		if string(got) != "known content" {
			t.Fatalf("Get = %q, want %q", got, "known content")
		}
	})

	t.Run("unknown ref", func(t *testing.T) {
		_, err := s.Get("sha256:does-not-exist")
		if !errors.Is(err, memory.ErrUnknownRef) {
			t.Fatalf("Get error = %v, want %v", err, memory.ErrUnknownRef)
		}
	})

	t.Run("mutating a Get result does not affect the store", func(t *testing.T) {
		got, err := s.Get(ref)
		if err != nil {
			t.Fatalf("Get error = %v, want nil", err)
		}
		got[0] = 'X'
		got2, err := s.Get(ref)
		if err != nil {
			t.Fatalf("second Get error = %v, want nil", err)
		}
		if string(got2) != "known content" {
			t.Fatalf("Get = %q, want %q; Get must return a copy", got2, "known content")
		}
	})
}
