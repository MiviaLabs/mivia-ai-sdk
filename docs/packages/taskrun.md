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

## Sentinel errors

The package declares eight sentinels. Each is an `errors.New` value.

- `ErrNoLedger` — `Options.Ledger` is nil.
- `ErrNoOwner` — `Options.Owner` is empty.
- `ErrNoActor` — `Options.Actor` is empty.
- `ErrNoLease` — `Options.Lease` is not positive.
- `ErrNoKey` — `Task.Key` is empty.
- `ErrTaskDone` — the key is already `StatusCompleted`.
- `ErrTaskFailed` — the key is already `StatusFailed`.
- `ErrTaskBlocked` — the key is already `StatusBlocked`.

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