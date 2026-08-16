package envelope

import (
	"os"
	"testing"
)

// FuzzDecode feeds arbitrary bytes to Decode. It must never panic, and
// anything it accepts must re-encode cleanly. Run: go test -fuzz=FuzzDecode
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		"testdata/vectors/valid_minimal.json",
		"testdata/vectors/valid_full.json",
		"testdata/vectors/valid_signed.json",
	} {
		data, err := os.ReadFile(seed)
		if err != nil {
			f.Fatalf("read seed: %v", err)
		}
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte("{}"))
	f.Add([]byte(`{"version":"v1"`))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data)
		if err != nil {
			return
		}
		if _, err := m.Encode(); err != nil {
			t.Fatalf("decoded but cannot re-encode: %v", err)
		}
	})
}
