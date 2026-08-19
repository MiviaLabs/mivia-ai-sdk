// Fault-injection decorators shared by the e2e scenarios.

package e2e

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// ErrFault is the error every fault decorator wraps on its injected
// call. A scenario asserts a failing run with errors.Is.
var ErrFault = errors.New("e2e: injected fault")

// fault counts one call and reports whether it is the injected fault
// call. FaultOn is 1-based; zero never faults. The count is atomic, so
// concurrent seams fault exactly once.
func fault(calls *atomic.Int32, on int32) bool {
	return calls.Add(1) == on
}

// faultErr names the seam that faulted and wraps ErrFault.
func faultErr(seam string) error {
	return fmt.Errorf("e2e: %s fault: %w", seam, ErrFault)
}

// FaultStore wraps a ledger.Store and faults one call. The FaultOn-th
// call returns an error wrapping ErrFault; the HangOn-th call blocks
// until ctx is done, then returns ctx.Err(); every other call passes
// through unchanged. Zero on a mode disables it.
type FaultStore struct {
	// Store is the wrapped ledger.Store. Required.
	Store ledger.Store
	// FaultOn is the 1-based call to fail. Zero disables faults.
	FaultOn int32
	// HangOn is the 1-based call to block until ctx is done. Zero disables.
	HangOn int32

	calls atomic.Int32
}

// hang blocks until ctx is done on the HangOn-th call. Zero disables
// hang. It reports whether the call hung.
func (f *FaultStore) hang(ctx context.Context, n int32) bool {
	if f.HangOn != 0 && n == f.HangOn {
		<-ctx.Done()
		return true
	}
	return false
}

// Load faults or hangs on its target call, else passes through.
func (f *FaultStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	n := f.calls.Add(1)
	if f.hang(ctx, n) {
		return ledger.TaskState{}, false, ctx.Err()
	}
	if n == f.FaultOn {
		return ledger.TaskState{}, false, faultErr("ledger store")
	}
	return f.Store.Load(ctx, key)
}

// CompareAndSwap faults or hangs on its target call, else passes
// through.
func (f *FaultStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	n := f.calls.Add(1)
	if f.hang(ctx, n) {
		return false, ctx.Err()
	}
	if n == f.FaultOn {
		return false, faultErr("ledger store")
	}
	return f.Store.CompareAndSwap(ctx, key, old, new)
}

// Range faults or hangs on its target call, else passes through.
func (f *FaultStore) Range(ctx context.Context, fn func(ledger.TaskState) bool) error {
	n := f.calls.Add(1)
	if f.hang(ctx, n) {
		return ctx.Err()
	}
	if n == f.FaultOn {
		return faultErr("ledger store")
	}
	return f.Store.Range(ctx, fn)
}

// FaultNotifier wraps a channel.Notifier and faults one call. The
// FaultOn-th ask returns an error wrapping ErrFault; every other ask
// passes through.
type FaultNotifier struct {
	// Notifier is the wrapped channel. Required.
	Notifier channel.Notifier
	// FaultOn is the 1-based ask to fail. Zero disables faults.
	FaultOn int32

	calls atomic.Int32
}

// Notify faults on the target ask, else passes through. It satisfies
// channel.Notifier, so a caller assigns the method value.
func (f *FaultNotifier) Notify(ctx context.Context, q channel.Question) (channel.Answer, error) {
	if fault(&f.calls, f.FaultOn) {
		return channel.Answer{}, faultErr("channel notifier")
	}
	return f.Notifier(ctx, q)
}

// FaultCompleter wraps a provider.Completer and faults one call. The
// FaultOn-th Chat or ChatStream call returns an error wrapping
// ErrFault; every other call passes through.
type FaultCompleter struct {
	// Completer is the wrapped provider. Required.
	Completer provider.Completer
	// FaultOn is the 1-based method call to fail. Zero disables.
	FaultOn int32

	calls atomic.Int32
}

// Name returns a fixed label; it never faults.
func (f *FaultCompleter) Name() string { return "fault-completer" }

// Chat faults on the target call, else passes through.
func (f *FaultCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if fault(&f.calls, f.FaultOn) {
		return provider.Response{}, faultErr("provider completer")
	}
	return f.Completer.Chat(ctx, req)
}

// ChatStream faults on the target call, else passes through.
func (f *FaultCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if fault(&f.calls, f.FaultOn) {
		return nil, faultErr("provider completer")
	}
	return f.Completer.ChatStream(ctx, req)
}

// FaultWait wraps an agent.AckWait and faults one call. The FaultOn-th
// ack resolution returns an error wrapping ErrFault; every other call
// passes through.
type FaultWait struct {
	// Inner is the wrapped agent.AckWait. Required.
	Inner agent.AckWait
	// FaultOn is the 1-based ack to fail. Zero disables faults.
	FaultOn int32

	calls atomic.Int32
}

// Wait faults on the target ack, else passes through. It satisfies
// agent.AckWait, so a caller assigns the method value.
func (f *FaultWait) Wait(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	if fault(&f.calls, f.FaultOn) {
		return envelope.Ack{}, faultErr("agentrun wait")
	}
	return f.Inner(ctx, msg)
}

// HangCompleter is a provider.Completer whose Chat and ChatStream block
// until ctx is done, then return ctx.Err(). It models a provider whose
// response never arrives unless the caller cancels, so a scenario
// asserts a runner with a deadline surfaces the timeout, not a hang.
type HangCompleter struct{}

// Name returns a fixed label.
func (h *HangCompleter) Name() string { return "hang-completer" }

// Chat blocks until ctx is done, then returns ctx.Err().
func (h *HangCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

// ChatStream blocks until ctx is done, then returns ctx.Err().
func (h *HangCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
