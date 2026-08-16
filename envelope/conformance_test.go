package envelope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConformanceVectors pins the wire contract. Files in
// testdata/vectors: valid_* must decode (and verify, when signed);
// invalid_decode_* must fail Decode; invalid_sig_* must decode but fail
// VerifySignature. Add a vector whenever the schema or a rule changes.
func TestConformanceVectors(t *testing.T) {
	entries, err := os.ReadDir("testdata/vectors")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata/vectors", name))
			if err != nil {
				t.Fatalf("read vector: %v", err)
			}
			m, decErr := Decode(data)
			switch {
			case strings.HasPrefix(name, "valid_"):
				if decErr != nil {
					t.Fatalf("valid vector rejected: %v", decErr)
				}
				if m.Signature != "" {
					if err := m.VerifySignature(); err != nil {
						t.Fatalf("signed valid vector fails verification: %v", err)
					}
				}
			case strings.HasPrefix(name, "invalid_decode_"):
				if decErr == nil {
					t.Fatal("invalid vector decoded without error")
				}
			case strings.HasPrefix(name, "invalid_sig_"):
				if decErr != nil {
					t.Fatalf("invalid_sig vector must still decode: %v", decErr)
				}
				if err := m.VerifySignature(); err == nil {
					t.Fatal("tampered vector passed verification")
				}
			default:
				t.Fatal("vector name must start with valid_, invalid_decode_, or invalid_sig_")
			}
		})
	}
}
