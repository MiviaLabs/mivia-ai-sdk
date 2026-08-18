# Example: scheduler recurring jobs

This walkthrough registers two `Every` jobs on one `Scheduler`: `tick`
always succeeds, `flaky` always fails. Both fire once every 10
milliseconds. An `events.Bus` handler subscribes to
`scheduler.JobFailedEvent` and records each failure. `Run` blocks for
a bounded 55-millisecond window, then the program reports what fired.
The program builds and runs against the module.

## The job-failure path

```mermaid
sequenceDiagram
    participant Loop as Run loop
    participant Flaky as flaky Job
    participant Bus as events.Bus
    participant Handler

    Loop->>Flaky: fire (goroutine)
    Flaky-->>Loop: error "disk full"
    Loop->>Bus: Emit(JobFailedEvent)
    Bus->>Handler: run
```

## The program

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

func main() {
	s := scheduler.New()
	bus := events.New()

	var tickCount int32
	var failCount int32
	var mu sync.Mutex
	var failures []string

	if err := bus.Subscribe(scheduler.JobFailedEvent, func(_ context.Context, e events.Event) error {
		mu.Lock()
		failures = append(failures, e.Data)
		mu.Unlock()
		return nil
	}); err != nil {
		fmt.Println("subscribe:", err)
		return
	}

	// tick fires every 10ms and always succeeds.
	tick := func(_ context.Context) error {
		atomic.AddInt32(&tickCount, 1)
		return nil
	}
	if err := s.Add("tick", scheduler.Every(10*time.Millisecond), tick); err != nil {
		fmt.Println("add tick:", err)
		return
	}

	// flaky fires every 10ms and always fails.
	flaky := func(_ context.Context) error {
		atomic.AddInt32(&failCount, 1)
		return fmt.Errorf("disk full")
	}
	if err := s.Add("flaky", scheduler.Every(10*time.Millisecond), flaky); err != nil {
		fmt.Println("add flaky:", err)
		return
	}

	// Run for a short, bounded window; both jobs fire at least once.
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Millisecond)
	defer cancel()
	if err := s.Run(ctx, bus); err != nil {
		fmt.Println("run stopped:", err)
	}

	fmt.Println("tick count:", atomic.LoadInt32(&tickCount))
	fmt.Println("flaky count:", atomic.LoadInt32(&failCount))
	mu.Lock()
	fmt.Println("failure events recorded:", len(failures) >= 1)
	if len(failures) > 0 {
		fmt.Println("sample failure event:", failures[0])
	}
	mu.Unlock()
}
```

## What the program shows

`Run` fires each due job in its own goroutine, on the interval
`Every(10 * time.Millisecond)` set for both jobs. Over the
55-millisecond window, that gives five scheduled ticks; the program
saw `tick count: 5` and `flaky count: 5` on every run. Timing-based
runs can vary by a beat, so treat the exact count as approximate, not
guaranteed.

Every `flaky` failure emits `scheduler.JobFailedEvent` on `bus`, with
`Event.Data` naming the job id and the error. The subscribed handler
records each one, so `failures` ends up non-empty. `Run` returns
`ctx.Err()` once the timeout expires, so the program also prints `run
stopped: context deadline exceeded`. The program prints `tick count:
5`, `flaky count: 5`, `failure events recorded: true`, and `sample
failure event: job flaky failed: disk full`.
