# Phase 34: ledger durable admission and claim primitive

Status: revision round 4, ready to plan review. New top-level package.
Depends on the shipped `machine` and `events` packages only; `ledger`
does not import `heartbeat` (see the Scope note on staleness below).
Composes, once both land, with `durablefence`'s `Scenario` harness
(`docs/plans/agents/phase33_durablefence_kit.md`) to prove this
package's claim, takeover, and fence invariants without a second copy
of that test logic, and with `flow`'s retry policy
(`docs/plans/agents/phase30_flow_retry.md`) as a caller that retries a
step in-process while `ledger.Admit` guards the same task against a
second, out-of-process retry. Phase 33 names this package as its
first intended adopter for exactly this reason. Neither dependency is
required to build this phase; this phase's own tests prove its
invariants directly, and a later phase may replace or supplement them
with `durablefence` checks once that package ships. See
`docs/plans/agents/PHASES.md`.

## Goal

Give a caller a durable-task-admission primitive: a task submitted
under an idempotency key is admitted exactly once, even under retry or
duplicate submission, and its ownership can move between processes
through a lease with periodic renewal and a fenced takeover. A single
staleness signal, the lease deadline, decides when a claim is stale
enough to take over. A failed task marks its dependents blocked.

## Scope

This plan covers one phase and one package. The package carries five
tightly coupled concerns — admission, lease ownership, fencing,
dependency blocking, and snapshot persistence — because each concern
shares the same `TaskState` record and the same `Store.CompareAndSwap`
primitive; splitting them into separate phases would split one atomic
state machine across plan boundaries. Whether to split the phase
itself into smaller reviewable units is a separate, open question
tracked outside this document.

### What this is, and what it is not

`ledger` is not `flow`. `flow` runs one in-process graph of steps to
completion in one call; its state lives on the Go stack for the
duration of `Run`. `ledger` records the admission and ownership of
independent tasks across process restarts and across a worker pool. A
`flow.Run` invocation can itself be the task body a `ledger` owner
claims and executes; the two compose, but neither wraps the other.

`ledger` is not `machine`. `machine` defines one status model and its
legal moves for a single subject. `ledger` reuses `machine.Status` and
a `machine.Definition` internally to name and validate its five task
states (`StatusPending`, `StatusClaimed`, `StatusCompleted`,
`StatusFailed`, `StatusBlocked`) and their transitions, instead of
inventing a second enum and a second transition table. `ledger` adds
the parts `machine` does not have: an idempotency key, a claim lease,
a fence token, and dependency-driven blocking.

`ledger` is not the `durablefence` conformance kit
(`docs/plans/agents/phase33_durablefence_kit.md`). That kit is a
reusable `Scenario` test harness for proving concurrency invariants —
clock skew, lease races, network partition simulation — across any
claim-lease implementation. `ledger` is the first implementation the
kit names as an intended adopter. This phase does not build the kit
and does not depend on it existing; `ledger`'s own tests in this phase
prove its invariants directly. A later phase may wire a `Scenario`
against `ledger`'s `Claim`, `Renew`, and `Takeover` in
`ledger/ledger_test/`, replacing or supplementing this phase's
hand-written race test with the shared harness.

`ledger` is not `flow`'s retry policy
(`docs/plans/agents/phase30_flow_retry.md`). That phase teaches
`flow.Run` to re-run a failed step under a backoff schedule inside one
process, one graph walk. `ledger.Admit` is the mechanism a distributed
caller uses to make an out-of-process retry (a whole new process
picking up the task) safe: the second attempt with the same
idempotency key is rejected, not re-run. The two retry concerns are
different scopes; a caller may use both, layered: `flow` retries a
step in-process, and if the process dies mid-step, a second process
calls `ledger.Takeover` and resumes ownership.

### In scope

- An idempotency-keyed admission table with compare-and-swap
  semantics: `Admit` records a task once per key.
- A sequence watermark per key, so a late or duplicate submission with
  an equal or lower sequence number never re-admits, while a genuinely
  newer submission can rebase the record. Rebase applies only to a
  `StatusPending` or `StatusClaimed` record; a higher-sequence
  submission against a terminal record (`StatusCompleted`,
  `StatusFailed`, `StatusBlocked`) never rebases it.
- An ownership claim with a time-boxed lease and a fenced takeover.
  `TaskState.LeaseUntil` compared against the caller-supplied `now` is
  the single staleness signal for both `Claim` and `Takeover`: a
  second claimant cannot acquire a lease while `LeaseUntil` is still
  ahead of `now`; once `now` passes `LeaseUntil`, any caller, including
  the original owner, may claim it again through `Takeover`. The old
  claim's fence token is invalidated at that point, so a late write
  from the dispossessed owner is rejected. `ledger` tracks no separate
  liveness signal; it does not import `heartbeat`.
- Transitive blocking: completing a task as failed marks every task
  that named it as a dependency `StatusBlocked`, recursively.
- A pluggable `Store` for the admission and claim records, with an
  in-memory default. `ledger` ships no database client and no network
  transport.
- A `Snapshot`, `Encode`, and `Decode` triple for the in-memory
  default's state, so a caller using `MemStore` can persist a
  point-in-time copy in its own storage and restore it after a
  restart. This mirrors `flow`'s `Checkpoint` pattern from phase 25.
- Events on the caller-supplied `events.Bus`, nil-safe, for admission,
  claim, renewal, release, takeover, completion, and blocking. This
  mirrors the emit pattern `machine` and `flow` already carry.

### Out of scope, and why

- A shipped persistence backend. The SDK is stdlib-only; a real
  cross-process durability guarantee needs a database or a
  distributed store this SDK cannot ship. `ledger` defines the
  `Store` interface the guarantee needs and ships only the in-memory
  implementation used by default and by its own tests. A caller who
  needs the task record itself to survive a process crash — not only
  a graceful `Snapshot` — implements `Store` against their own
  database. This is not speculative: the interface is the seam the
  cross-process claim exists to cross, not an unused abstraction.
- Distributed consensus. `ledger` does not elect a leader and does not
  replicate its `Store` across nodes. A caller who needs consensus
  runs `Store` against a store that already provides it (for example
  a database with a compare-and-swap primitive), and `ledger`'s own
  `CompareAndSwap` calls compose with that guarantee without knowing
  it exists.
- Network transport. Moving a claim or an admission request between
  processes over a wire is a transport concern, already assigned to
  `a2aclient` for the agent-to-agent case. `ledger` is a local API
  each process calls against its own `Store` handle; how a caller
  wires that handle to a shared backend is the caller's transport
  choice.
- Scheduling and prioritization. `ledger` records which task is
  admitted, owned, and blocked. It does not choose which pending task
  a caller should claim next. A caller's own worker-pool loop applies
  its own priority policy on top of `ledger.State`.
- A generic subagent-pool or worker-pool type. This package is a
  general durable-admission primitive. A caller composes it into
  their own pool; `ledger` names no pool, no dispatcher, and no
  subagent concept.
- History replay and event sourcing. `Snapshot` stores one row per
  task, the current state, not an append-only log. A caller who needs
  a replayable history keeps their own log of the events `ledger`
  emits on the bus.

## API

- `type IdempotencyKey string` — the caller-chosen key that dedupes a
  task across retries and duplicate submissions.
- `type OwnerID string` — the caller-chosen identity of a claimant.
- `type Sequence uint64` — the watermark a caller assigns per
  submission; `Admit` rejects a sequence at or below the recorded one.
- `type FenceToken uint64` — a monotonic counter `Claim` and
  `Takeover` return. `Renew`, `Release`, and `Complete` reject a stale
  token, so a dispossessed owner's late call never mutates the record.
- `const StatusPending, StatusClaimed, StatusCompleted, StatusFailed, StatusBlocked machine.Status`
  — the five task states, reusing `machine.Status` rather than a new
  enum.
- `type TaskState struct { Key IdempotencyKey; Status machine.Status; Sequence Sequence; Owner OwnerID; Fence FenceToken; LeaseUntil time.Time; Needs []IdempotencyKey; BlockedBy IdempotencyKey; Task any; Rev uint64 }`
  — the full record for one key. `Task` is caller-owned, like
  `machine.InOut.Input`; `ledger` never inspects it. `Rev` is a
  `Store`-assigned revision counter, not a caller-chosen value: every
  `Ledger` method that mutates a record reads `Rev` off the loaded
  record and forwards it unchanged inside the `old` argument to
  `CompareAndSwap`; `Ledger` never sets or interprets `Rev` itself. See
  the `Store` entry below for why `Rev` exists and how it resolves the
  blind spot a `(Sequence, Status, Fence)` compare key leaves for
  `Renew`.
- `func (s TaskState) Validate() error` — rejects an empty `Key`, a
  `Status` outside the five constants, a `Needs` entry equal to `Key`
  (a direct self-cycle), a non-empty `BlockedBy` when `Status` is not
  `StatusBlocked`, an empty `BlockedBy` when `Status` is
  `StatusBlocked`, and a `StatusClaimed` record with an empty `Owner`
  or a zero `LeaseUntil`. `Snapshot.Validate` and `Decode` call this
  method on every entry, so a decoded record carries the same
  guarantees as one built through `Admit` or `Claim`. A two-hop or
  longer cycle (`A.Needs` names `B`, `B.Needs` names `A`) spans two
  separate `Admit` calls and cannot be caught by a single-record
  `Validate`; the walk that would otherwise loop over such a cycle
  guards itself with a visited-key set instead. See the `Complete`
  entry below.
- `type Store interface { Load(ctx context.Context, key IdempotencyKey) (TaskState, bool, error); CompareAndSwap(ctx context.Context, key IdempotencyKey, old TaskState, new TaskState) (bool, error); Range(ctx context.Context, fn func(TaskState) bool) error }`
  — the pluggable record backend. `CompareAndSwap` with a zero-value
  `old` means insert-if-absent. A conforming `Store` compares `old`
  against the stored record on `(Sequence, Status, Fence, Rev)`, not on
  full-struct or `Task`-field equality: `Task` is a caller-owned `any`
  value with no defined equality across implementations, and a real
  backend keys its compare-and-swap off a version or sequence column,
  not a full-row comparison. `Rev` exists because `(Sequence, Status,
  Fence)` alone is blind to a `Renew` call: `Renew` only extends
  `LeaseUntil`, so two concurrent `Renew` calls on the same key and
  fence read the identical `(Sequence, Status, Fence)` triple, and a
  three-field compare key lets both `CompareAndSwap` calls succeed in
  sequence, silently dropping the first writer's lease extension. `Rev`
  closes that gap: on every successful `CompareAndSwap`, a conforming
  `Store` sets the stored record's `Rev` to one more than the stored
  record's prior `Rev` (a newly inserted record starts at `Rev` zero),
  regardless of which other fields the write changed. Because the
  second `Renew`'s `old.Rev` still carries the pre-write value, its
  `CompareAndSwap` call finds the stored `Rev` already bumped by the
  first `Renew` and fails, exactly like any other conflicting write.
  `MemStore` and every other `Store` implementation must apply the same
  four-field comparison and the same bump-on-every-write rule, so a
  caller can swap backends without a behavior change. Every `Ledger`
  method that mutates a record, including `Renew`, participates in this
  counter: none of them is exempt from detecting a concurrent write to
  the same record. `Range` supports the dependent scan and `Snapshot`.
  `fn` must not call any other `Store` method on the same `Store` from
  inside the callback: `Range` may hold a lock for the duration of the
  iteration, and a reentrant `Load` or `CompareAndSwap` call from within
  `fn` can deadlock against it. A caller that needs to mutate records
  found during a `Range` scan collects the matching keys in `fn`, lets
  `Range` return, then mutates each key in a second pass. `Complete`'s
  dependent-blocking walk follows this two-pass shape: see the
  `Complete` entry below.

  Every `Ledger` method that calls `CompareAndSwap` (`Admit`, `Claim`,
  `Renew`, `Release`, `Takeover`, `Complete`) follows the same
  retry-and-reclassify contract on a losing call: when
  `CompareAndSwap` returns `false, nil`, the method re-loads the
  current record through `Load` and re-evaluates its own eligibility
  rule against that fresh record, rather than surfacing a generic
  CAS-failure result. Two outcomes follow. If the fresh record is no
  longer eligible for the requested move (for example a different
  owner now holds a live lease, or the record already reached the
  target state), the method returns immediately through its own
  declared sentinel error (for example `Claim` and `Takeover` resolve
  to `ErrLeaseActive` or `ErrNotStale`, never a raw "compare-and-swap
  failed" error). If the fresh record is still eligible (for example
  `Renew` racing a second `Renew` from the same owner and fence, whose
  `CompareAndSwap` lost only because the first `Renew` already bumped
  `Rev`, with every other compared field unchanged), the method
  retries `CompareAndSwap` against the freshly loaded record, repeating
  the reload-reclassify-retry cycle until it succeeds, becomes
  ineligible, or the caller's `ctx` is canceled or its deadline passes,
  in which case the method returns `ctx.Err()`. This means a losing
  `Claim` or `Takeover` always surfaces through its declared sentinel
  error, while a losing `Renew`, `Release`, or `Complete` against a
  record the caller still legitimately owns keeps retrying until its
  write lands, so no legitimate lease extension, release, or completion
  is silently dropped by a same-owner race, and no illegitimate one is
  silently accepted.
- `type MemStore struct{}` and `func NewMemStore() *MemStore` — the
  shipped in-memory `Store`, mutex-guarded. Default when `New` gets a
  nil `Store`.
- `type Ledger struct{}` and `func New(store Store, bus *events.Bus) (*Ledger, error)`
  — `store` nil defaults to `NewMemStore()`; `bus` nil disables
  events, matching the `flow` and `machine` emit contract.
- `func (l *Ledger) Admit(ctx context.Context, key IdempotencyKey, seq Sequence, task any, needs ...IdempotencyKey) (bool, error)`
  — CAS-admits a `StatusPending` record when the key is absent or the
  stored sequence is lower and the stored `Status` is `StatusPending`
  or `StatusClaimed`. Returns `false, nil`, not an error, when the key
  already holds a record at or above `seq`, or when the stored record
  is terminal (`StatusCompleted`, `StatusFailed`, `StatusBlocked`): a
  duplicate or late-arriving submission is a no-op against a finished
  task, not a failure, so a retrying caller never branches on an error
  type to detect its own retry and never resurrects a finished result.
- `func (l *Ledger) Claim(ctx context.Context, key IdempotencyKey, owner OwnerID, lease time.Duration, now time.Time) (FenceToken, error)`
  — claims a `StatusPending` record, or a `StatusClaimed` record whose
  `LeaseUntil` is at or before `now`. `LeaseUntil` versus `now` is the
  only staleness signal `Claim` reads; `ledger` keeps no heartbeat or
  other liveness state. Bumps `FenceToken` and sets `LeaseUntil` to
  `now.Add(lease)`. `Claim` first calls `Store.Load`. Returns
  `ErrNoKey` when the key has no record, checked before any status
  precondition and before any `CompareAndSwap` call: a never-admitted
  key is never eligible for `Claim`, so `Claim` never falls through to
  `Store`'s zero-value, insert-if-absent `CompareAndSwap` convention to
  create a `StatusClaimed` record outside `Admit`. Only once `Load`
  reports the key found does `Claim` evaluate `Status` and
  `LeaseUntil`. Returns `ErrLeaseActive` when another owner's
  `LeaseUntil` is still after `now`. On a losing `CompareAndSwap`,
  re-loads the record and re-evaluates the same precondition, so the
  loser of a race against another `Claim` or a `Takeover` also returns
  `ErrLeaseActive`, per the `Store` retry-and-reclassify contract
  above.
- `func (l *Ledger) Renew(ctx context.Context, key IdempotencyKey, owner OwnerID, fence FenceToken, lease time.Duration, now time.Time) error`
  — extends `LeaseUntil` to `now.Add(lease)`. `Renew` first calls
  `Store.Load`. Returns `ErrNoKey` when the key has no record, checked
  before the `ErrNotClaimed` and `ErrFenced` checks and before any
  `CompareAndSwap` call, so `Renew` cannot mistake "never admitted" for
  "admitted but not currently claimed". Returns `ErrNotClaimed` when
  the record's `Status` is not `StatusClaimed`. Returns
  `ErrFenced` when `fence` does not match the current record. On a
  losing `CompareAndSwap` (for example a concurrent `Renew` on the same
  fence that won the `Rev` bump first), re-loads the record and
  re-evaluates the same two checks: if the fence no longer matches or
  the record left `StatusClaimed`, returns the matching sentinel error;
  otherwise the record is still legitimately owned by the caller, so
  `Renew` retries `CompareAndSwap` against the fresh record instead of
  losing the extension, per the `Store` retry-and-reclassify contract
  above.
- `func (l *Ledger) Release(ctx context.Context, key IdempotencyKey, owner OwnerID, fence FenceToken) error`
  — returns a claimed record to `StatusPending`. `Release` first calls
  `Store.Load`. Returns `ErrNoKey` when the key has no record, checked
  before the `ErrFenced` and `ErrNotClaimed` checks and before any
  `CompareAndSwap` call. Returns `ErrFenced`
  on a stale token. Returns `ErrNotClaimed` when the record's `Status`
  is not `StatusClaimed`. On a losing `CompareAndSwap` whose fresh
  reload still shows the caller's fence owning a `StatusClaimed`
  record, retries `CompareAndSwap` against the fresh record; a fresh
  reload showing a fence mismatch or a `Status` no longer
  `StatusClaimed` returns the matching sentinel error instead, per the
  `Store` retry-and-reclassify contract above.
- `func (l *Ledger) Takeover(ctx context.Context, key IdempotencyKey, owner OwnerID, lease time.Duration, now time.Time) (FenceToken, error)`
  — claims a `StatusClaimed` record whose `LeaseUntil` is at or before
  `now`: the same staleness signal `Claim` reads, applied to the same
  `Store.CompareAndSwap` call, so a `Claim` and a `Takeover` racing on
  the same just-expired lease are resolved by one CAS, not by two
  independent eligibility checks that could disagree. Bumps
  `FenceToken` past the prior value, fencing the dispossessed owner's
  token. `Takeover` first calls `Store.Load`. Returns `ErrNoKey` when
  the key has no record, checked before the `ErrNotStale` and
  `ErrNotClaimed` checks and before any `CompareAndSwap` call: a
  never-admitted key is distinct from an admitted `StatusPending`
  record, and `Takeover` reports that distinction instead of folding
  it into `ErrNotClaimed`. Returns `ErrNotStale` when `LeaseUntil` is still after `now`.
  Returns `ErrNotClaimed` for a `StatusPending` or terminal record:
  `Takeover` never admits or claims a never-claimed record; a caller
  uses `Claim` for that. On a losing `CompareAndSwap`, re-loads the
  record and re-evaluates the same eligibility rule, so the loser of a
  race against another `Takeover` or a `Claim` also returns
  `ErrNotStale` or `ErrNotClaimed` as appropriate, per the `Store`
  retry-and-reclassify contract above.
- `func (l *Ledger) Complete(ctx context.Context, key IdempotencyKey, owner OwnerID, fence FenceToken, status machine.Status) error`
  — accepts only `StatusCompleted` or `StatusFailed`. Returns
  `ErrUnknownStatus` when `status` is neither `StatusCompleted` nor
  `StatusFailed` (including the zero value, `StatusPending`, and
  `StatusBlocked`), checked first, before any `Store` call: an invalid
  `status` argument is a caller error independent of the record's
  state, so `Complete` rejects it before spending a `Load`. Once
  `status` is valid, `Complete` calls `Store.Load`. Returns `ErrNoKey`
  when the key has no record, checked next, before the `ErrFenced` and
  `ErrNotClaimed` checks and before any `CompareAndSwap` call. Requires
  the record's `Status` to be `StatusClaimed`. Returns `ErrFenced` on a
  stale token. Returns `ErrNotClaimed` when the record's `Status` is
  not `StatusClaimed`, including a record `Complete` already moved to
  `StatusCompleted` or `StatusFailed`: a second call against a
  terminal record never mutates it, even when the fence still matches.
  On a losing `CompareAndSwap` for the primary key, re-loads the
  record and re-evaluates both checks, mirroring `Renew`: if the fence
  no longer matches, returns `ErrFenced`; if the fence matches but the
  fresh record's `Status` is no longer `StatusClaimed` (for example a
  racing duplicate `Complete` call already won and moved the record to
  a terminal status), returns `ErrNotClaimed` instead of retrying;
  only when the fence still matches and the fresh record's `Status` is
  still `StatusClaimed` does `Complete` retry `CompareAndSwap` against
  the fresh record, per the `Store` retry-and-reclassify contract
  above. This closes the gap the general contract's own example
  names: "the record already reached the target state" is not
  eligible for retry, so a losing `Complete` can never silently flip a
  terminal record from one finished status to the other. On `StatusFailed`, walks the dependency
  graph and sets `StatusBlocked` with `BlockedBy` set to the failed
  key on every record that (transitively) names it in `Needs`. The
  walk keeps a visited-key set and skips a key already visited, so a
  cycle across two or more `Admit` calls (for example `A.Needs`
  contains `B`, `B.Needs` contains `A`, or a longer chain) terminates
  instead of looping, and each affected record is blocked exactly
  once. Pass two's `CompareAndSwap` skips a collected key whose loaded
  record already has `Status` `StatusBlocked`: `BlockedBy` names
  whichever failed key blocked the record first, and a later,
  unrelated failure that also transitively reaches the same dependent
  never overwrites it. The walk touches `Store` in exactly
  two passes, per the `Store.Range` reentrancy rule above: one `Range`
  call, then one `CompareAndSwap` call per affected key. Pass one calls
  `Range` once; its `fn` makes no eligibility decision and calls no
  other `Store` method, only copies every visited record into an
  in-memory list. Between the two `Store` passes, after `Range`
  returns, an in-memory step walks that list to compute transitive
  `Needs` membership: starting from `key`, it repeatedly scans the list
  for records naming an already-blocked key in `Needs`, adding each
  newly found key to a visited-key set, until a scan finds no new key.
  This step makes no further `Store` calls. Pass two then calls
  `CompareAndSwap` on each key in the final visited-key set (excluding
  `key` itself). Computing transitive membership needs the full
  in-memory list: it cannot be decided from a single record while
  `Range`'s callback is still iterating.
- `func (l *Ledger) State(ctx context.Context, key IdempotencyKey) (TaskState, bool, error)`
  — the current record for a key. The bool is a found signal: `true`
  when `key` has a record, `false` when it does not. `State` never
  returns an error for a missing key; only `Load` failing against the
  `Store` returns an error.
- `func (l *Ledger) Blocked(ctx context.Context, key IdempotencyKey) (IdempotencyKey, bool, error)`
  — the blocking ancestor when `key`'s status is `StatusBlocked`.
  `Blocked` does not add a separate found-bool signal: its bool means
  "`key` is currently blocked", and that answer is `false` both for a
  never-admitted key and for an admitted, unblocked key, because
  `Blocked` only ever answers one question, not two. A caller who needs
  to tell "never admitted" apart from "admitted but not blocked" calls
  `State` first; `Blocked` is read-only and performs no `CompareAndSwap`,
  so it carries none of the insert-if-absent risk `Claim`, `Renew`,
  `Release`, `Takeover`, and `Complete` guard against with `ErrNoKey`.
- `type Snapshot struct { Tasks []TaskState }` and
  `func (l *Ledger) Snapshot(ctx context.Context) (Snapshot, error)`
  — a point-in-time copy of every record in `Store`, gathered through
  `Range`.
- `func (s Snapshot) Validate() error` — runs `TaskState.Validate` over
  every entry.
- `func (s Snapshot) Encode() ([]byte, error)` and
  `func Decode(data []byte) (Snapshot, error)` — validate, then
  `encoding/json` marshal or unmarshal, matching `flow.Checkpoint`'s
  pattern.
- `func (l *Ledger) Restore(ctx context.Context, s Snapshot) error`
  — inserts every `Snapshot` record into `Store` through
  `CompareAndSwap`. Meant for `MemStore` cold-start or test setup; a
  caller whose `Store` already talks to durable storage needs no
  `Restore` call, since the next `Load` reads the durable record
  directly.
- `const AdmittedEvent, ClaimedEvent, RenewedEvent, ReleasedEvent, TakenOverEvent, CompletedEvent, BlockedEvent events.Name`
  — `"ledger.admitted"`, `"ledger.claimed"`, `"ledger.renewed"`,
  `"ledger.released"`, `"ledger.taken_over"`, `"ledger.completed"`,
  `"ledger.blocked"`. Emitted after the matching `CompareAndSwap`
  succeeds; never before.
- `var ErrLeaseActive, ErrFenced, ErrNotStale, ErrNotClaimed, ErrNoKey, ErrUnknownStatus error`
  — sentinel errors, checked with `errors.Is`.

## Tests

Test files live in `ledger/ledger_test/`:

- `admit_test.go` — red-green cases for `Admit`:
  - The first `Admit` for a key returns `true, nil` and stores
    `StatusPending`.
  - A second `Admit` at the same or a lower sequence returns
    `false, nil`; the stored record is unchanged.
  - A second `Admit` at a higher sequence against a `StatusPending` or
    `StatusClaimed` record rebases the stored record and returns
    `true, nil`.
  - A second `Admit` at a higher sequence against a terminal record
    (`StatusCompleted`, `StatusFailed`, or `StatusBlocked`) returns
    `false, nil` and leaves the stored record unchanged: a late
    resubmission never resurrects a finished task.
  - `Admit` records `Needs` for later blocking lookups.
  - `AdmittedEvent` fires once per successful admission, never on a
    rejected duplicate and never on a rejected post-completion
    resubmission.
- `claim_test.go` — red-green cases for `Claim`, `Renew`, `Release`:
  - `Claim` on a pending record succeeds and returns a nonzero
    `FenceToken`.
  - A second `Claim` on the same key while the lease is live returns
    `ErrLeaseActive`; the first owner's fence is unchanged.
  - `Claim` after the lease has expired, called by a different owner,
    succeeds and bumps the fence.
  - `Renew` with the current fence extends `LeaseUntil`.
  - `Renew` with a stale fence returns `ErrFenced`.
  - `Renew` against a `StatusPending` record (never claimed) returns
    `ErrNotClaimed`.
  - `Release` with the current fence returns the record to
    `StatusPending`; a subsequent `Claim` by any owner succeeds.
  - `Release` with a stale fence returns `ErrFenced`.
  - `Release` against a `StatusPending` record (never claimed) returns
    `ErrNotClaimed`.
  - `Claim` against a never-admitted key returns `ErrNoKey`; the
    `Store` gains no record, proving `Claim` never uses the
    insert-if-absent `CompareAndSwap` convention to create a record
    outside `Admit`.
  - `Renew` against a never-admitted key returns `ErrNoKey`, not
    `ErrNotClaimed`: the two cases stay distinguishable.
  - `Release` against a never-admitted key returns `ErrNoKey`, not
    `ErrNotClaimed`.
- `takeover_test.go` — red-green cases for `Takeover`:
  - `Takeover` while `LeaseUntil` is still after `now` returns
    `ErrNotStale`.
  - `Takeover` called with a `now` at or after `LeaseUntil` succeeds,
    bumps the fence, and records the new owner.
  - `Takeover` against a `StatusPending` record (never claimed) returns
    `ErrNotClaimed`; `Claim`, not `Takeover`, is required to admit
    ownership of a never-claimed record.
  - `Takeover` against a terminal record (`StatusCompleted`) returns
    `ErrNotClaimed`.
  - `Takeover` against a never-admitted key returns `ErrNoKey`, not
    `ErrNotClaimed`: a never-admitted key and an admitted
    `StatusPending` record stay distinguishable.
  - A `Renew` call from the old owner's fence, made after a
    successful `Takeover`, returns `ErrFenced`. This is the fencing
    guarantee: the dispossessed owner cannot mutate the record after
    losing it.
  - `TakenOverEvent` fires once, naming the new owner.
- `complete_test.go` — red-green cases for `Complete` and blocking:
  - `Complete` with `StatusCompleted` and the current fence marks the
    record done; a `Renew` after completion returns `ErrNotClaimed`.
  - `Complete` with `StatusFailed` on a key that other pending records
    name in `Needs` marks every dependent `StatusBlocked`, with
    `BlockedBy` set to the failed key.
  - A two-level dependency chain: failing the root blocks both the
    direct dependent and the dependent's own dependent, transitively.
  - A cycle case: `A.Needs` contains `B`, `B.Needs` contains `A`.
    `Complete(A, StatusFailed)` terminates, and both `A` and `B` end
    up `StatusBlocked` exactly once, proving the visited-key set stops
    the walk from looping.
  - A three-hop cycle case: `A.Needs` contains `B`, `B.Needs` contains
    `C`, `C.Needs` contains `A`. `Complete(A, StatusFailed)`
    terminates, and `A`, `B`, and `C` each end up `StatusBlocked`
    exactly once.
  - A shared-dependent case: two independently admitted records, `X`
    and `Y`, both name `D` in `Needs`. `Complete(X, StatusFailed)`
    then `Complete(Y, StatusFailed)` blocks `D` exactly once, and `D`'s
    `BlockedBy` is deterministic: it names whichever of `X` or `Y`
    completed as failed first, and the second `Complete` call leaves
    `D`'s `BlockedBy` unchanged.
  - `Complete` with a stale fence returns `ErrFenced`; no dependent is
    blocked.
  - `Complete` against a `StatusPending` record (never claimed) returns
    `ErrNotClaimed`.
  - A second `Complete` call against a record `Complete` already moved
    to `StatusCompleted`, made with the same owner and the same fence
    the first call used, returns `ErrNotClaimed` and leaves the record
    at `StatusCompleted`: a matching fence on a terminal record is not
    enough to reclassify the call as a legitimate retry.
  - `Complete` against a never-admitted key returns `ErrNoKey`, not
    `ErrNotClaimed`.
  - `Complete` called with `status` set to `StatusPending` (an
    out-of-range choice for `Complete`, neither `StatusCompleted` nor
    `StatusFailed`) against an already-claimed record returns
    `ErrUnknownStatus`, and the stored record is unchanged: `Complete`
    rejects the argument before any `Store` call, so a bad `status`
    never touches a legitimately claimed record.
  - `Complete` called with the zero-value `machine.Status` against a
    never-admitted key returns `ErrUnknownStatus`, not `ErrNoKey`,
    proving the `status` check runs first, before `Store.Load`.
  - `Blocked` on an unblocked key returns `false`.
  - `Blocked` on a never-admitted key returns `false, nil`, the same
    result as an admitted, unblocked key: `Blocked` carries no separate
    found-bool signal, per the API section's decision on `Blocked`.
- `snapshot_test.go` — red-green cases for `Snapshot`, `Encode`,
  `Decode`, `Restore`:
  - `Snapshot` after several admissions lists every record.
  - `Encode` then `Decode` round-trips every field.
  - `Decode` rejects malformed JSON and an out-of-range `Status`.
  - `Restore` on a fresh `Ledger` reproduces every prior `State`
    lookup result.
- `mem_store_test.go` — `MemStore.CompareAndSwap` rejects a call whose
  `old`'s `(Sequence, Status, Fence, Rev)` tuple does not match the
  stored record's, even when other fields (for example `Task`) differ;
  accepts a call whose `old` is the zero value against an absent key.
  `MemStore.CompareAndSwap` bumps the stored `Rev` by one on every
  successful call, including a call whose `new` value changes only
  `LeaseUntil` (a `Renew`-shaped write), and a newly inserted record
  starts at `Rev` zero. A second `CompareAndSwap` call whose `old.Rev`
  still carries the pre-write value fails after a first call already
  bumped `Rev`, even when that first call changed no other field. A
  `MemStore.Range` call whose `fn` populates a slice from a store
  with several records visits every record exactly once and returns
  `nil`, proving `Range` completes normally when `fn` does not call
  back into `Store` (the reentrancy case is exercised through
  `complete_test.go`'s two-level and cycle cases, which drive the real
  `Complete` two-pass walk against a populated `MemStore`).
- `admit_integration_test.go` — an end-to-end sequence: admit, claim,
  renew twice, complete failed, and assert a dependent's blocked
  state, all through one `Ledger` backed by `MemStore`, with a real
  `events.Bus` collecting every emitted event in order.
- `claim_race_test.go` — an integration test with two goroutines
  racing `Claim` on the same freshly admitted key. Assert exactly one
  goroutine's `Claim` succeeds and the other observes `ErrLeaseActive`.
  Run under `go test -race`.
- `admit_race_test.go` — an integration test with N goroutines calling
  `Admit` on the same key and the same sequence number concurrently.
  Assert exactly one goroutine's `Admit` returns `true, nil` and every
  other returns `false, nil`; assert `AdmittedEvent` fires exactly
  once. Run under `go test -race`.
- `takeover_race_test.go` — an integration test that admits and claims
  a key with a short lease, waits until `now` is past `LeaseUntil`,
  then races one goroutine calling `Claim` against another calling
  `Takeover` on the same key at the same `now`. Assert exactly one of
  the two calls succeeds and the other returns its rejection error
  (`ErrLeaseActive` or `ErrNotStale`), proving the single underlying
  `Store.CompareAndSwap` call decides the winner rather than either
  method's own eligibility check racing the other's. Run under
  `go test -race`.
- `renew_race_test.go` — an integration test that admits and claims a
  key with a short lease, then races two goroutines both calling
  `Renew` with the same owner and fence at overlapping times. Assert
  both goroutines' `Renew` calls return `nil`: the `Rev` counter makes
  the second goroutine's first `CompareAndSwap` attempt fail even
  though its `old` carries the identical `(Sequence, Status, Fence)`
  triple as the first goroutine's, proving the conflict is detected;
  the retry-and-reclassify contract then makes that goroutine reload,
  find the record still owned under the same fence, and retry until it
  succeeds, so neither caller sees a spurious error and the second
  write is never silently dropped. Assert `RenewedEvent` fires exactly
  twice, once per successful call. A wrapped `Store` that counts
  `CompareAndSwap` calls asserts the total exceeds two, proving at
  least one retry happened. Run under `go test -race`.
- `complete_race_test.go` — an integration test that admits and claims
  a key, then races two goroutines both calling `Complete` with the
  same owner and fence: one with `status` `StatusCompleted`, the other
  with `status` `StatusFailed`. Assert exactly one goroutine's
  `Complete` call returns `nil` and the other returns `ErrNotClaimed`;
  assert the final stored record's `Status` matches whichever call won,
  never the loser's requested status. This proves a losing `Complete`
  reclassifies against the fresh record's `Status`, not only its
  fence, so a racing duplicate call can never silently flip a terminal
  record from one finished status to the other. Assert `CompletedEvent`
  fires exactly once. Run under `go test -race`.
- `admit_bench_test.go` — a benchmark measuring `Admit` throughput
  against `MemStore` under increasing key counts. Report ops/sec and
  allocs/op; no fixed allocation budget, since `MemStore`'s internal
  locking varies with `GOMAXPROCS`, per the exception
  `docs/plans/agents/PHASES.md` allows for goroutine-dependent counts.

## Verification

`make verify` passes. `go test -race ./...` covers `claim_race_test.go`,
`admit_race_test.go`, `takeover_race_test.go`, `renew_race_test.go`,
`complete_race_test.go`, and any other concurrent case. The coverage
floor for `ledger` holds at or above 85.

`api/ledger.txt` is created by `make api-update` from the API section
above, and the diff lands in the same change as the code.
`policy/layers.json` gains the row `"ledger": ["machine", "events"]` in
the same change that adds the package directory; the plan lists it
here so the row exists before any code imports it. `ledger` does not
import `heartbeat`: `LeaseUntil` versus the caller-supplied `now` is
the only staleness signal, read directly off `TaskState` by both
`Claim` and `Takeover` through the same `Store.CompareAndSwap` call.

`docs/architecture.md` gains `ledger` in the module map, with an
import edge from `ledger` to `machine` and `events`.
`docs/packages/ledger.md` documents the exported surface. `AGENTS.md`
gains a `ledger/` layout bullet naming the admission, claim, lease,
fence, and blocked vocabulary.

No conformance vector changes: `ledger` carries no signed or threaded
wire form, so `envelope/testdata/vectors/` is untouched.
`docs/protocol-design.md` is untouched for the same reason.
