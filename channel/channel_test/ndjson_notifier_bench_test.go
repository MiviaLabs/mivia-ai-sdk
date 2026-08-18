package channel_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// ndjsonBenchAllocBudget bounds one NewNDJSONNotifier call round
// trip's allocation count. Measured baseline: 28 allocs/op without
// -race, 31 allocs/op under -race (the race detector's own shadow
// bookkeeping adds a few), see the comment on
// BenchmarkNDJSONNotifierRoundTrip. json.NewEncoder's per-call
// allocation, the bufio.Scanner's fixed initial buffer, and the
// background goroutine and channel writeQuestion now spawns to stay
// ctx-aware on the write side are the expected allocation sources;
// the budget adds a small margin above the -race baseline, enough to
// catch a regression of a few unexpected allocs.
const ndjsonBenchAllocBudget = 36

// BenchmarkNDJSONNotifierRoundTrip measures one NewNDJSONNotifier
// call round trip over an io.Pipe pair with a fixture goroutine
// answering immediately. Measured baseline on the development
// machine: ~14400 ns/op, 28 allocs/op.
func BenchmarkNDJSONNotifierRoundTrip(b *testing.B) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var q ndjsonLine
			if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
				return
			}
			reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: "ok"}
			if err := json.NewEncoder(aw).Encode(reply); err != nil {
				return
			}
		}
	}()

	ctx := context.Background()
	q := channel.Question{ID: "bench-q", Recipient: "human", Payload: "proceed?"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := notify(ctx, q); err != nil {
			b.Fatalf("notify() error = %v, want nil", err)
		}
	}
}

// TestNDJSONNotifierRoundTripAllocBudget asserts one round trip's
// allocation count stays within ndjsonBenchAllocBudget, calibrated
// with a small margin above the measured baseline in
// BenchmarkNDJSONNotifierRoundTrip's comment, enough to catch a real
// regression such as a repeated bufio.Scanner allocation per call.
func TestNDJSONNotifierRoundTripAllocBudget(t *testing.T) {
	pr, pw := io.Pipe()
	ar, aw := io.Pipe()
	notify := channel.NewNDJSONNotifier(ar, pw)

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var q ndjsonLine
			if err := json.Unmarshal(sc.Bytes(), &q); err != nil {
				return
			}
			reply := ndjsonLine{Type: "answer", QuestionID: q.ID, Approved: true, Payload: "ok"}
			if err := json.NewEncoder(aw).Encode(reply); err != nil {
				return
			}
		}
	}()

	ctx := context.Background()
	q := channel.Question{ID: "bench-q", Recipient: "human", Payload: "proceed?"}

	allocs := testing.AllocsPerRun(20, func() {
		if _, err := notify(ctx, q); err != nil {
			t.Fatalf("notify() error = %v, want nil", err)
		}
	})
	if allocs > ndjsonBenchAllocBudget {
		t.Fatalf("AllocsPerRun = %v, want <= %d", allocs, ndjsonBenchAllocBudget)
	}
}
