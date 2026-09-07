package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// agentloopSurface drives the turn-shaping surface an external Tool
// wrapper sees: the per-turn dispatch ledger, the steering handle, the
// context probes, and the explicit zero-value error policy.
func agentloopSurface() error {
	order := agentloop.NewBatchOrder([]int{0, 1})
	if got := order.Dispatched(); len(got) != 2 {
		return fmt.Errorf("Dispatched = %v, want two indices", got)
	}
	if order.Settled(0) {
		return fmt.Errorf("Settled(0) = true, want false before settlement")
	}
	if !order.UnsettledBefore(1) {
		return fmt.Errorf("UnsettledBefore(1) = false, want true while index 0 is unsettled")
	}
	order.Settle(0)
	if order.UnsettledBefore(1) {
		return fmt.Errorf("UnsettledBefore(1) = true, want false after settling index 0")
	}
	select {
	case <-order.Changed():
		return fmt.Errorf("Changed fired before any settlement")
	default:
	}
	if _, ok := agentloop.BatchOrderFromContext(context.Background()); ok {
		return fmt.Errorf("BatchOrderFromContext found a ledger on a bare context")
	}
	if _, ok := agentloop.ToolCallFromContext(context.Background()); ok {
		return fmt.Errorf("ToolCallFromContext found a call on a bare context")
	}
	steer := agentloop.NewSteer()
	if steer.HasActiveCall() {
		return fmt.Errorf("HasActiveCall = true on a fresh Steer")
	}
	steer.SetInjector(func() []provider.Message { return nil })
	// A caller that selects the report policy writes it explicitly or
	// leaves the field unset; both name the same zero value.
	opts := agentloop.Options{OnToolError: agentloop.ErrorPolicyReport}
	if opts.OnToolError != agentloop.ErrorPolicyReport {
		return fmt.Errorf("OnToolError = %q, want the report policy", opts.OnToolError)
	}
	return nil
}
