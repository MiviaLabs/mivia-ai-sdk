package agentrun_test

import (
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
)

// TestArtifactsConcurrent proves Set and Get are safe when called
// concurrently from multiple goroutines. It does not assert logical
// correctness; it asserts that concurrent access never panics or races.
func TestArtifactsConcurrent(t *testing.T) {
	a := &agentrun.Artifacts{}
	const n = 100

	var wg sync.WaitGroup
	wg.Add(2)

	// Writers race Set across all step IDs.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			step := "step-" + string(rune(i%26))
			a.Set(step, string(rune('a'+i%26)))
		}
	}()

	// Readers race Get alongside writers.
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			step := "step-" + string(rune(i%26))
			_, _ = a.Get(step)
		}
	}()

	wg.Wait()

	// If we reach here without a panic, concurrent access is safe.
	// A data-race detector (go test -race) will catch lock errors.
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
