# Plan: contextsession

Status: planned, not yet built. Carves the durable session planner out of `contextplan` into `contextsession`.

## Goal

Fit durable session history into a bounded provider request with retention and spooling.

## Scope

Inside:

- One new package: `contextsession`.
- Session planning: `Planner` and `NewPlanner`.
- Planning outcome: `PlanResult`, `Elision`, `ElisionReason`, and elision constants.
- Content truncation for compliance: `StubContent` and `StubContentBytes`.
- Admit state machine: folded into an unexported `planState` struct with an `admit` method.
- `planState` is allocated per `Plan` call, so `Planner` remains stateless and safe for concurrent use.
- Sentinels: `ErrNilStore`, `ErrNilSession`.
- Integration with `contextplan`, `contextref`, `contextstate`, `provider`, and `spool`.

Outside:

- Cache parameter: dropped `cache *memory.Store` and `ErrNilCache` because cache writes were unread.
- Token budget compaction and window types. `contextplan` owns `Window`, `Compaction`, and `Compact`.
- Reasoning event detection. `contextplan.IsReasoningEvent` owns detection; `contextsession` imports it.
- Canonical content reference minting. `contextref` owns the minter.
- Concrete provider implementations. Callers supply a `provider.TokenEstimator`.

## API

The exported surface will land in `api/contextsession.txt` via `make api-update`:

```go
package contextsession

const (
    ElisionReasonWindowOverflow    ElisionReason = "window_overflow"
    ElisionReasonRetentionExpired  ElisionReason = "retention_expired"
    ElisionReasonReasoningRedacted ElisionReason = "reasoning_redacted"
    ElisionReasonRevoked           ElisionReason = "revoked"
    StubContentBytes                             = 256
)

func NewPlanner(store *contextstate.MemStore, spooler *spool.Spool) (*Planner, error)
func (p *Planner) Plan(ctx context.Context, sess *contextstate.Session, w contextplan.Window, e provider.TokenEstimator) (PlanResult, error)
func StubContent(content []byte) []byte

type Elision struct {
    Ref      contextstate.ContentRef
    Reason   ElisionReason
    Kept     int
    SpoolRef string
}

type ElisionReason string

type PlanResult struct {
    Request         provider.Request
    Elisions        []Elision
    EstimatedTokens int
}

type Planner struct {
}

var (
    ErrNilSession = errors.New("contextsession: session must not be nil")
    ErrNilStore   = errors.New("contextsession: store must not be nil")
)
```

Design rationale:

- `contextsession` isolates the durable session planner from lightweight token compaction.
- It imports `contextplan` for `Window` and `IsReasoningEvent`.
- It drops `memory.Store` caching because `contextplan.Planner` only performed unread writes.
- It depends on `contextstate` for payload records and `spool` for overflow storage.
- `planState` holds running admission state for one `Plan` execution, preserving thread safety.

## Tests

Tests will live in `contextsession/contextsession_test/`:

- `plan_test.go`:
  - Entire session fits inside token budget with no elisions.
  - Oldest source events drop when token budget overflows.
  - Compliance payload keeps a stub message when aged out.
  - Reasoning events are elided and excluded from messages.
  - Revoked payload produces an elision without failing planning.
  - Reserved headroom is preserved for model responses.
  - Compliance stubs that exceed budget are dropped cleanly.
  - Multiple back-to-back compliance stubs at budget boundary.
  - Unrecognized retention classes receive standard overflow drops.
  - Revocation after warm cache is immediately visible.
  - Revoked compliance payloads never receive stubs.
  - Revoked reasoning events are marked revoked, not reasoning.
  - Sessions with every event revoked produce clean empty requests.
  - Concurrent `Plan` executions on shared `Planner` under race detector.
- `elision_test.go`:
  - Verify stub truncation does not cut multi-byte UTF-8 runes.
- `plan_spool_test.go`:
  - Overflow elisions write to spool storage when configured.
- `plan_resolution_test.go`:
  - Verify payload resolution and revocation handling without cache assumptions.

## Verification

- `policy/layers.json` gains `"contextsession": ["contextplan", "contextref", "contextstate", "provider", "spool"]`.
- `policy/pending_wiring.json` records `contextsession`:
  ```json
  "contextsession": {
    "reason": "Durable session planner carved out of contextplan. Intended caller is external application code adapting Plan to agentloop.Options.Trim.",
    "target": "external application code (outside this module)",
    "permanent": true
  }
  ```
- `api/contextsession.txt` is generated via `make api-update`.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_deps.py` passes.
- `python3 scripts/check_orphan_packages.py` passes.
- `python3 scripts/check_prose.py docs/plans/contextsession.md` passes.
- `python3 scripts/check_labels.py` passes.
- Test coverage reaches at least 85 percent.
