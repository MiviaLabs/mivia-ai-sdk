# Package reference: contextsession

`contextsession` plans durable session history into a bounded provider request. It resolves payloads from durable storage, enforces retention rules, and spools elided content. The exported surface below mirrors `api/contextsession.txt`.

## Types

- `Planner` — fits session history into a bounded request. Safe for concurrent use.
- `PlanResult` — planning outcome: `Request`, `Elisions`, and `EstimatedTokens`.
- `Elision` — details one drop or trim decision. Tracks `Ref`, `Reason`, `Kept`, and `SpoolRef`.
- `ElisionReason` — reason for payload elision: `ElisionReasonWindowOverflow`, `ElisionReasonRetentionExpired`, `ElisionReasonReasoningRedacted`, `ElisionReasonRevoked`.

## Functions and methods

- `NewPlanner(store, spooler)` — constructs a `Planner` over durable `store` and optional `spooler`. Returns `ErrNilStore` when store is nil.
- `Planner.Plan(ctx, sess, w, e)` — plans session events into a provider request. Re-reads payload status on each call. Handles revocations, reasoning redaction, compliance stubs, and overflow spooling.
- `StubContent(content)` — truncates compliance payload to `StubContentBytes`. Appends an elision marker while preserving valid UTF-8 boundaries.
- `IsReasoningEvent(e)` — reports whether event kind matches `provider.ReasoningEventKind`.

## Constants

- `StubContentBytes` (256) — maximum byte length for retained compliance stubs.
- `ElisionReasonWindowOverflow` ("window_overflow") — elision due to token budget overflow.
- `ElisionReasonRetentionExpired` ("retention_expired") — compliance payload trimmed to stub.
- `ElisionReasonReasoningRedacted` ("reasoning_redacted") — reasoning event excluded from active context.
- `ElisionReasonRevoked` ("revoked") — payload denied by durable store revocation.

## Failure modes

Use `errors.Is` to test these errors:
- `ErrNilStore` — `NewPlanner` returns it when `store` is nil.
- `ErrNilSession` — `Plan` returns it when `sess` is nil.

## Invariants

- Every source event resolves payload metadata directly from the store.
- Revoked payloads never enter `Request.Messages` and always produce `ElisionReasonRevoked`.
- Reasoning events never enter `Request.Messages` and produce `ElisionReasonReasoningRedacted`.
- Only `RetentionCompliance` payloads receive stubs on budget overflow; others are dropped.
- Overflow and retention drops spool full payload bytes when a spooler is configured.

## Cross-references

- [contextplan.md](contextplan.md) — token window definitions and compaction policies.
- [contextstate.md](contextstate.md) — session event logs and durable payload storage.
- [spool.md](spool.md) — durable overflow storage for elided content.
- [provider.md](provider.md) — request models and token estimator interface.
