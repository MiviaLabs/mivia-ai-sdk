package agentrun_test

import (
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
)

// TestArtifactsConcurrent proves Set and Get are safe when called
// concurrently from multiple goroutines, and that every written key is
// present once the goroutines join. The -race detector catches any lock
// error; the final assertions catch lost writes without it.
func TestArtifactsConcurrent(t *testing.T) {
	a := &agentrun.Artifacts{}
	const n = 100

	var wg sync.WaitGroup
	wg.Add(2)

	// Writers race Set across all step IDs.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			step := "step-" + string(rune('a'+i%26))
			a.Set(step, string(rune('a'+i%26)))
		}
	}()

	// Readers race Get alongside writers.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			step := "step-" + string(rune('a'+i%26))
			_, _ = a.Get(step)
		}
	}()

	wg.Wait()

	// Every key written by the concurrent setters must be present once
	// the goroutines join. This asserts real stored state, not just the
	// absence of a panic, so the test proves something without -race.
	for i := 0; i < 26; i++ {
		step := "step-" + string(rune('a'+i))
		if _, ok := a.Get(step); !ok {
			t.Fatalf("artifact %q missing after concurrent writes", step)
		}
	}
}

// TestArtifactsNilReceiver proves Set and Get on a nil pointer do not panic.
func TestArtifactsNilReceiver(t *testing.T) {
	var nilA *agentrun.Artifacts
	nilA.Set("k", "v") // must not panic
	_, ok := nilA.Get("k")
	if ok {
		t.Fatal("nil Artifacts.Get returned true, want false")
	}
}
