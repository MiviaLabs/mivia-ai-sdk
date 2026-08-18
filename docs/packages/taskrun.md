# Package reference: taskrun

The taskrun package is the ledger ceremony as one call. A caller runs
one task under admission, claim, and completion. The caller supplies
the work and every identity value; the package supplies the ceremony.
The exported surface below mirrors `api/taskrun.txt`.

## Types

- `Options` — the required inputs for one `Run` call: `Ledger`, `Actor`,
  `Owner`, `Lease`, and the optional `Now` clock. Every field is
  required except `Now`, which defaults to `time.Now`.
- `Task` — one idempotent submission: `Key`, `Seq`, `Description`, and
  the `Needs` dependency keys. `Key` and `Seq` are the ledger admission
  identity; `Description` becomes the stored task payload and `Needs`
  the dependency list.

## Functions

- `Run(ctx, opts, t, work)` — admits `t`, checks its terminal status,
  claims it, runs `work`, and completes it with the mapped status. The
  returned error is the work's own error, unwrapped, when work ran. It
  returns a sentinel for a task already terminal in the ledger. A
  `Complete` failure joins the returned error.

## Failure modes

The package declares eight sentinels. Each is an `errors.New` value.

- `ErrNoLedger` ("taskrun: ledger is required") — `Run` returns it when
  `Options.Ledger` is nil. Pinned by
  `taskrun/taskrun_test/options_test.go`.
- `ErrNoOwner` ("taskrun: owner is required") — `Run` returns it when
  `Options.Owner` is empty. Pinned by
  `taskrun/taskrun_test/options_test.go`.
- `ErrNoActor` ("taskrun: actor is required") — `Run` returns it when
  `Options.Actor` is empty. Pinned by
  `taskrun/taskrun_test/options_test.go`.
- `ErrNoLease` ("taskrun: lease must be positive") — `Run` returns it
  when `Options.Lease` is not positive. Pinned by
  `taskrun/taskrun_test/options_test.go`.
- `ErrNoKey` ("taskrun: task key is required") — `Run` returns it when
  `Task.Key` is empty. Pinned by
  `taskrun/taskrun_test/options_test.go`.
- `ErrTaskDone` ("taskrun: task already completed") — `Run` returns it
  when the key is already recorded `StatusCompleted`. Pinned by
  `taskrun/taskrun_test/replay_test.go`.
- `ErrTaskFailed` ("taskrun: task already failed") — `Run` returns it
  when the key is already recorded `StatusFailed`. Pinned by
  `taskrun/taskrun_test/replay_test.go`.
- `ErrTaskBlocked` ("taskrun: task blocked on a failed dependency") —
  `Run` returns it when the key is recorded `StatusBlocked` on a
  failed dependency. Pinned by `taskrun/taskrun_test/blocked_test.go`.

## Run order

`Run` validates `Options` and `Task` before any ledger call. It calls
`Admit` with `Description` and `Needs`. A duplicate admit is not an
error. It reads `State` and returns the sentinel for a terminal status
without running work. It calls `Claim` with `Owner` and `Lease`. A live
lease surfaces an error satisfying `errors.Is(err,
ledger.ErrLeaseActive)`. It runs `work`. Success completes
`StatusCompleted`; a work error completes `StatusFailed` and returns
the work error unwrapped. A `Complete` failure joins the returned error
with the work result leading.

## Invariants

- Validation runs before any ledger call, in the order the sentinels
  list.
- The package holds no state between calls. Every value arrives through
  `Options` and `Task`.
- The work runs at most once per `Run` call. A terminal key never runs
  work.