# Phase 50: taskrun ledger ceremony

Status: future. Plan-only; it has not gone through plan review yet.
Depends on no unshipped phase. It adds one new top-level package.

## Why this phase exists

Every durable caller repeats the same ledger ceremony: `Admit`,
`Claim`, run the work, `Complete` with the mapped status. The
compiled example performs it by hand
(`docs/examples/_agentcomposition/main.go:131` and `main.go:224`).
The error path is easy to forget. A forgotten `Complete` leaves the
task claimed for the whole lease.

The ceremony is mechanical. The failure mapping is always the same:
a work error completes `StatusFailed`; success completes
`StatusCompleted`. No caller should retype it.

## Goal

One function runs one task under ledger admission. It completes the
task on every path the lease still owns. The caller supplies the
work; the package supplies the ceremony.

## Scope

Inside:

- New package `taskrun` with `Options`, `Task`, `Run`, and sentinels.
- Admission with dependency keys, claim with lease and fence, work
  execution, completion with the mapped status.
- Idempotent replay: a task already completed or failed returns its
  sentinel without running the work. Both statuses are terminal in
  the ledger; re-running either requires a new idempotency key.
- A claim blocked by a live lease returns `ledger.ErrLeaseActive`
  wrapped, with the key, for the caller's takeover decision.

Outside:

- Lease renewal. The caller sizes the lease to the work. The limit is
  documented, and renewal waits for a caller whose work outlives a
  short lease.
- Any `Takeover` policy. Stale takeover stays a caller decision; the
  package reports the blocked claim and returns.
- Any import beyond `ledger`. The work function's signature takes
  only `context.Context`, so any caller composes `agentrun`, `agent`,
  or plain code inside it.

## API

```go
package taskrun

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

// Run admits, claims, and completes one task around work. The
// returned error is the work's own error, unwrapped, when work ran.
func Run(ctx context.Context, opts Options, t Task,
	work func(ctx context.Context) error) error

var (
	ErrNoLedger  = errors.New("taskrun: ledger is required")
	ErrNoOwner   = errors.New("taskrun: owner is required")
	ErrNoActor   = errors.New("taskrun: actor is required")
	ErrNoLease   = errors.New("taskrun: lease must be positive")
	ErrNoKey     = errors.New("taskrun: task key is required")
	ErrTaskDone  = errors.New("taskrun: task already completed")
	ErrTaskFailed = errors.New("taskrun: task already failed")
)
```

Run order:

1. Validate options and task; return the first sentinel.
2. `Admit` with `Description` and `Needs`. A duplicate admit is not
   an error; proceed.
3. `State` on the key: `StatusCompleted` returns `ErrTaskDone`;
   `StatusFailed` returns `ErrTaskFailed`. Neither runs work, and
   neither can be re-claimed: the ledger holds both as terminal.
4. `Claim` with `Owner` and `Lease`. `ErrLeaseActive` returns
   wrapped, with no `Complete`.
5. Run `work`. A nil error completes `ledger.StatusCompleted`; an
   error completes `ledger.StatusFailed` and returns the work error
   unwrapped.
6. A `Complete` failure joins the returned error; the work result
   still leads.

## Validation

- `Options` and `Task` checks run before any ledger call, in the
  order listed above.
- `Now` defaults to `time.Now` inside `Run`; a caller-supplied clock
  makes the lease math testable.
- The package holds no state between calls. Every value it needs
  arrives through `Options` and `Task`.

## Tests

`taskrun/taskrun_test/`, one external test package:

- `options_test.go`, table-driven: every sentinel above, plus the
  `Now` default.
- `ceremony_test.go`: happy path asserts the event order on a real
  bus: `AdmittedEvent`, `ClaimedEvent`, `CompletedEvent`.
- `failure_test.go`: a work error returns unwrapped, completes
  `StatusFailed`, and a `State` read confirms it.
- `complete_always_test.go`: a work function fails on purpose after
  the claim; it does not panic. A second `Complete` with the same
  fence returns `ledger.ErrNotClaimed`, proving the first landed.
- `replay_test.go`: a completed key returns `ErrTaskDone` and never
  calls work; a failed key returns `ErrTaskFailed` and never calls
  work.
- `lease_test.go`: a pre-claimed key returns an error satisfying
  `errors.Is(err, ledger.ErrLeaseActive)` and no `CompletedEvent`.

## Verification

- `policy/layers.json` gains `"taskrun": ["ledger"]`.
- `make api-update` lands `api/taskrun.txt` in the same change.
- `make verify` passes; `taskrun` and the module total hold the 85
  floor.
- `docs/plans/taskrun.md`, `docs/packages/taskrun.md`, and a
  `docs/examples/taskrun.md` walkthrough ship with the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
