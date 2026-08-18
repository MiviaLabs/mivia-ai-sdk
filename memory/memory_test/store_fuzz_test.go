package memory_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

// FuzzPutGet feeds arbitrary byte content through Put on a
// fixed-budget Store. It proves no content panics Put or Get, that
// content within the budget always round-trips through Get byte for
// byte, and that Put's ref always equals envelope.ContextRef of the
// content, whether or not the content lands.
func FuzzPutGet(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("a"))
	f.Add([]byte("shared context blob"))
	f.Add(make([]byte, 64))
	f.Add([]byte{0x00, 0x01, 0xff})

	const budget = 64

	f.Fuzz(func(t *testing.T, content []byte) {
		s, err := memory.New(budget)
		if err != nil {
			t.Fatalf("New error = %v", err)
		}

		ref, err := s.Put(content)
		wantRef := envelope.ContextRef(string(content))

		if len(content) > budget {
			if err == nil {
				t.Fatalf("Put(%x) error = nil, want ErrBudgetExceeded", content)
			}
			if ref != "" {
				t.Fatalf("Put(%x) ref = %q, want empty on rejection", content, ref)
			}
			return
		}
		if err != nil {
			t.Fatalf("Put(%x) error = %v, want nil", content, err)
		}
		if ref != wantRef {
			t.Fatalf("Put(%x) ref = %q, want %q", content, ref, wantRef)
		}

		got, err := s.Get(ref)
		if err != nil {
			t.Fatalf("Get(%q) error = %v, want nil", ref, err)
		}
		if string(got) != string(content) {
			t.Fatalf("Get(%q) = %x, want %x", ref, got, content)
		}
	})
}
