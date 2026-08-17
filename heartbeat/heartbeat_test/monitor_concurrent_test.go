package heartbeat_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
)

// TestConcurrentBeatSameIDLatestWins runs N goroutines beating one id
// with strictly increasing at values, then joins. Alive must reflect
// the final (largest) at, proving concurrent Beats on one id
// serialize correctly instead of merely not crashing.
func TestConcurrentBeatSameIDLatestWins(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Hour
	m, err := heartbeat.New(timeout)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			at := base.Add(time.Duration(i) * time.Second)
			// A strictly increasing at can race a later goroutine's
			// earlier at; ErrStaleBeat is an expected benign outcome.
			_ = m.Beat("shared", at)
		}()
	}
	wg.Wait()

	final := base.Add(time.Duration(n-1) * time.Second)
	if !m.Alive("shared", final) {
		t.Fatalf("Alive(shared, final) = false, want true")
	}
	// now.Sub(last) must be zero: the stored last-seen time is the
	// largest at, none of the earlier goroutines' at values won.
	if now := final.Add(time.Nanosecond); !m.Alive("shared", now) {
		t.Fatalf("Alive(shared, final+1ns) = false, want true (last-seen must equal final at)")
	}
	past := final.Add(timeout + time.Second)
	if m.Alive("shared", past) {
		t.Fatalf("Alive(shared, past timeout from final) = true, want false")
	}
}

// TestConcurrentBeatDistinctIDsDeadSet runs N goroutines each beating
// a distinct id concurrently, some past the timeout and some not,
// then joins. Dead's result set must equal exactly the expected set
// of past-timeout ids, proving concurrent writes are all visible to
// a later read.
func TestConcurrentBeatDistinctIDsDeadSet(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Minute
	m, err := heartbeat.New(timeout)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	const n = 100
	now := base.Add(90 * time.Second)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("id-%03d", i)
			at := base
			if i%2 == 1 {
				// Odd ids beat close to now, so they stay alive.
				at = now.Add(-time.Second)
			}
			if err := m.Beat(id, at); err != nil {
				t.Errorf("Beat(%s) error = %v", id, err)
			}
		}()
	}
	wg.Wait()

	want := make([]string, 0, n/2)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			want = append(want, fmt.Sprintf("id-%03d", i))
		}
	}
	got := m.Dead(now)
	if !equalSlices(got, want) {
		t.Fatalf("Dead(now) mismatch: got %d ids, want %d ids", len(got), len(want))
	}
}
