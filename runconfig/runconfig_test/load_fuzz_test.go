package runconfig_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
)

// FuzzLoad feeds arbitrary bytes to runconfig.Load. It must never
// panic, and anything it accepts must be a non-nil Definition with a
// non-nil Plan and Machine; anything it rejects must wrap
// ErrBadDocument. Run: go test -fuzz=FuzzLoad ./runconfig/runconfig_test/
func FuzzLoad(f *testing.F) {
	seeds := []string{
		oneStepDoc("grep"),
		internalStepDoc("workspaceread"),
		goldenDoc,
		workspaceReadGoldenDoc,
		baseDoc,
		`{}`,
		`[]`,
		`{"machine":`,
		`{"machine": null, "plan": null}`,
		`{"machine": {"initial": "q", "transitions": []},
		  "plan": {"steps": [{"id": "s", "to": "d",
		  "retry": {"max_attempts": 1, "base_delay": "1ms", "max_delay": "1ms"},
		  "loop": {"max": 2},
		  "sub": {"steps": [{"id": "inner", "to": "d"}]}}]}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := runconfig.Load(data)
		if err != nil {
			if !errors.Is(err, runconfig.ErrBadDocument) {
				t.Fatalf("Load error %v does not wrap ErrBadDocument", err)
			}
			if d != nil {
				t.Fatalf("Load returned a non-nil Definition alongside an error")
			}
			return
		}
		if d == nil {
			t.Fatal("Load returned a nil Definition with a nil error")
		}
		if d.Plan == nil {
			t.Fatal("Load returned a nil Plan with a nil error")
		}
		if d.Machine == nil {
			t.Fatal("Load returned a nil Machine with a nil error")
		}
		if d.Blocks == nil {
			t.Fatal("Load returned nil Blocks with a nil error")
		}
		if d.External == nil {
			t.Fatal("Load returned a nil External registry with a nil error")
		}
	})
}
