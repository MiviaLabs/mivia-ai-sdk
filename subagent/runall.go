// RunAll and its result types.

package subagent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Spec names one runner and its starting record for RunAll.
type Spec struct {
	Name   string
	Runner *agentrun.Runner
	In     machine.InOut
}

// Result reports one RunAll member's outcome.
type Result struct {
	Name   string
	Status machine.Status
	Err    error
}

// RunAll runs every spec concurrently and joins, returning one
// result per spec in spec order. One member's error never cancels
// its siblings.
func RunAll(ctx context.Context, specs []Spec) []Result {
	results := make([]Result, len(specs))
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go func(i int, s Spec) {
			defer wg.Done()
			thread := fmt.Sprintf("%s-%d", s.Name, atomic.AddUint64(&threadSeq, 1))
			status, _, err := s.Runner.Run(ctx, thread, s.In)
			results[i] = Result{Name: s.Name, Status: status, Err: err}
		}(i, specs[i])
	}
	wg.Wait()
	return results
}
