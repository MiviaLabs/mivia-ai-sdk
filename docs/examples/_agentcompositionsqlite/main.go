//go:build ledger_sqlite

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// ledger identifiers fixed for this demo's single task, matching the
// default-tag program in ../_agentcomposition/main.go.
const (
	ledgerActor = ledger.Actor("composer")
	ledgerOwner = ledger.OwnerID("worker-1")
	ledgerKey   = ledger.IdempotencyKey("task-composition-1")
	ledgerLease = time.Minute
)

func main() {
	ctx := context.Background()
	now := time.Now()

	store, err := ledger.NewSQLiteStore(":memory:")
	if err != nil {
		fmt.Println("ledger.NewSQLiteStore:", err)
		return
	}
	defer store.Close()

	led, err := ledger.New(store, nil)
	if err != nil {
		fmt.Println("ledger.New:", err)
		return
	}

	if _, err := led.Admit(ctx, ledgerActor, ledgerKey, 1, "review invoice 42", now); err != nil {
		fmt.Println("ledger.Admit:", err)
		return
	}
	fence, err := led.Claim(ctx, ledgerActor, ledgerKey, ledgerOwner, ledgerLease, now)
	if err != nil {
		fmt.Println("ledger.Claim:", err)
		return
	}
	if err := led.Complete(ctx, ledgerActor, ledgerKey, ledgerOwner, fence, ledger.StatusCompleted, time.Now()); err != nil {
		fmt.Println("ledger.Complete:", err)
		return
	}

	state, _, err := led.State(ctx, ledgerKey)
	if err != nil {
		fmt.Println("ledger.State:", err)
		return
	}

	fmt.Println("ledger status:", state.Status)
}
