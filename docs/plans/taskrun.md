# Plan: taskrun

## Goal

The taskrun package runs one task under ledger admission. It wraps the
admit-claim-complete ceremony around a caller-supplied work function.
It supplies the ceremony; the tagless-to-ledger failure mapping stays a
caller-supplied work function.

## Scope

Inside:

- The `Options`, `Task`, and `Run` surface and the eight sentinel
  errors.
- Admission with dependency keys, claim with lease and fence, work
  execution, and completion with the mapped status.
- A blocked task returns `ErrTaskBlocked` without running work.
- Replay of a completed, failed, or blocked key returns its sentinel
  without running work.
- A claim blocked by a live lease returns an error satisfying
  `errors.Is(err, ledger.ErrLeaseActive)`.

Outside:

- Lease renewal. The caller sizes the lease to the work.
- Any `Takeover` policy. The package reports the blocked claim.
- Any import beyond `ledger`.

## API

```go
type Options struct {
	Ledger *ledger.Ledger
	Actor  ledger.Actor
	Owner  ledger.OwnerID
	Lease  time.Duration
	Now    func() time.Time // defaults to time.Now
}

type Task struct {
	Key         ledger.IdempotencyKey
	Seq         ledger.Sequence
	Description string
	Needs       []ledger.IdempotencyKey
}

func Run(ctx context.Context, opts Options, t Task,
	work func(ctx context.Context) error) error
```

The sentinel errors are `ErrNoLedger`, `ErrNoOwner`, `ErrNoActor`,
`ErrNoLease`, `ErrNoKey`, `ErrTaskDone`, `ErrTaskFailed`, and
`ErrTaskBlocked`. Every sentinel is an `errors.New` value.

`Run` validates `Options` and `Task` before any ledger call. It admits,
checks the terminal statuses, claims, and runs `work`. A nil work result
completes `StatusCompleted`; an error completes `StatusFailed` and
returns the work error unwrapped. A `Complete` failure joins the
returned error. `Now` defaults to `time.Now`. The package holds no state
between calls.

## Tests

Tests live in `taskrun/taskrun_test/`, one external package. The
following files exist:

- `options_test.go` — table-driven over every sentinel, the `Now`
  default, and the no-`AdmittedEvent` validation case.
- `ceremony_test.go` — event order on a real bus: `AdmittedEvent`,
  `ClaimedEvent`, `CompletedEvent`.
- `failure_test.go` — a work error returns unwrapped and completes
  `StatusFailed`.
- `blocked_test.go` — a blocked dependency returns `ErrTaskBlocked`
  and never runs work.
- `complete_always_test.go` — a failed work still completes; a second
  `Complete` with the same fence returns `ledger.ErrNotClaimed`.
- `complete_join_test.go` — a failing `Complete` joins the returned
  error; `errors.Is` matches the work error.
- `replay_test.go` — completed and failed keys return their sentinels
  without running work.
- `lease_test.go` — a live lease satisfies `errors.Is` for
  `ledger.ErrLeaseActive` and stages no `CompletedEvent`.
- `propagate_test.go` — an `Admit` and a `State` store failure return
  the store error, not a taskrun sentinel.

A `probeStore` wrapper fails a chosen `CompareAndSwap` or `Load` and
records the last fence.

## Verification

- `policy/layers.json` grants `taskrun` the `["ledger"]` edge.
- `make api-update` lands `api/taskrun.txt` in the same change.
- `make verify` passes; `taskrun` and the module total hold the 85
  floor.
- `python3 scripts/check_prose.py` and `scripts/check_labels.py` pass.