# Example: taskrun ledger ceremony

This walkthrough runs a `build` task through the `taskrun` ceremony.
The program admits the task, claims it under a lease, runs the work,
and completes it with the mapped status. It then walks a failed key and
shows the block-and-replay sentinels. The program builds and runs
against the module.

## The ceremony

The `taskrun.Run` call replaces the hand-written ledger ceremony. The
caller supplies the work; the package supplies admission, claim, and
completion. A successful work run completes `StatusCompleted`. A failed
work run completes `StatusFailed` and returns the work error.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

func main() {
	l, err := ledger.New(nil, nil) // nil Store defaults to MemStore
	if err != nil {
		fmt.Println("new:", err)
		return
	}

	ctx := context.Background()
	opts := taskrun.Options{
		Ledger: l,
		Actor:  "ci-runner",
		Owner:  "worker-1",
		Lease:  time.Minute,
	}

	// Run a successful build task.
	build := taskrun.Task{Key: "build", Seq: 1, Description: "build ok"}
	if err := taskrun.Run(ctx, opts, build, func(context.Context) error {
		return nil
	}); err != nil {
		fmt.Println("build:", err)
	}

	// Run a build task whose work fails.
	broke := taskrun.Task{Key: "broke", Seq: 1, Description: "build fails"}
	workErr := errors.New("compiler error")
	if err := taskrun.Run(ctx, opts, broke, func(context.Context) error {
		return workErr
	}); err != nil {
		fmt.Println("broke returned:", err == workErr)
	}

	// Replay a completed key returns its sentinel without running work.
	if err := taskrun.Run(ctx, opts, build, func(context.Context) error {
		fmt.Println("this work never runs")
		return nil
	}); err != nil {
		fmt.Println("replay:", err)
	}
}
```

## Output

The program prints `broke returned: true` and
`replay: taskrun: task already completed`. The third `Run` never runs
its work. The ledger holds the `build` key at `StatusCompleted` and
the `broke` key at `StatusFailed`.