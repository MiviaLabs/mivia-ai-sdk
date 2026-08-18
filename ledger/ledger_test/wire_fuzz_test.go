package ledger_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// FuzzDecode proves Decode never panics on arbitrary bytes and, on
// success, always returns a Snapshot whose Validate rules already
// passed: Decode calls Validate internally, so a decoded Snapshot
// with an invalid entry (an unknown Status, a self-referencing Needs
// entry, a mismatched BlockedBy) is a bug in Decode's own contract.
func FuzzDecode(f *testing.F) {
	seeds := []string{
		`{"Tasks":[]}`,
		`{"Tasks":[{"Key":"k1","Status":"pending"}]}`,
		`{"Tasks":[{"Key":"k1","Status":"claimed","Owner":"o1","LeaseUntil":"2024-01-01T00:00:00Z"}]}`,
		`{"Tasks":[{"Key":"k1","Status":"blocked","BlockedBy":"root"}]}`,
		`{"Tasks":[`, /* malformed */
		`{"Tasks":[{"Key":"k1","Status":"nonsense"}]}`,
		`not json at all`,
		``,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		snap, err := ledger.Decode([]byte(data))
		if err != nil {
			return
		}
		if verr := snap.Validate(); verr != nil {
			t.Fatalf("Decode returned a Snapshot that fails its own Validate: %v", verr)
		}
	})
}
