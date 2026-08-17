package envelope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAckConformanceVectors pins the ack wire contract. Files in
// testdata/ack_vectors: valid_ack_* must decode and re-encode to the
// same bytes; invalid_decode_ack_* must fail DecodeAck. The vectors
// live apart from testdata/vectors, which the message conformance test
// reads; an ack is not a message.
func TestAckConformanceVectors(t *testing.T) {
	entries, err := os.ReadDir("testdata/ack_vectors")
	if err != nil {
		t.Fatalf("read ack vectors: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("ack_vectors holds no vectors")
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata/ack_vectors", name))
			if err != nil {
				t.Fatalf("read vector: %v", err)
			}
			ack, decErr := DecodeAck(data)
			switch {
			case strings.HasPrefix(name, "valid_ack_"):
				if decErr != nil {
					t.Fatalf("valid ack vector rejected: %v", decErr)
				}
				back, err := ack.Encode()
				if err != nil {
					t.Fatalf("valid ack vector did not re-encode: %v", err)
				}
				if string(back) != strings.TrimRight(string(data), "\n") {
					t.Fatalf("re-encode changed the ack json:\n%s\nwant\n%s", back, data)
				}
			case strings.HasPrefix(name, "invalid_decode_ack_"):
				if decErr == nil {
					t.Fatal("invalid ack vector decoded without error")
				}
			default:
				t.Fatal("ack vector name must start with valid_ack_ or invalid_decode_ack_")
			}
		})
	}
}
