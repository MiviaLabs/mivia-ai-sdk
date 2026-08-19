# Phase 66: context planner

Status: plan only, not scheduled. One new composition package plus
one fold into `provider`. It depends on phase 65's types and on the
shipped `provider` interfaces.

## Why this phase exists

`mivia-agent`'s `internal/contextmgr` owns context preparation
policy: a planner that fits a session into a model's window, eliding
what does not fit and handing the elided content to a spool. It
calibrates token estimates with an exponentially weighted moving
average. It converts durable context into provider messages. It is
about eight thousand lines.

This SDK has the interfaces but not the engine. `provider` ships
`TokenEstimator`, `ContextAccountant`, and `ReasoningPolicy`.
`contextbudget` checks a static byte-and-event fit. Nothing plans a
session, nothing elides, nothing calibrates.

The external gap is documented: the OpenAI Agents SDK's sessions
cannot control context length on long conversations. That is this
phase's exact problem, already solved once in production next door.

The `reasoning` fold rides here because the planner consumes it.
`mivia-agent`'s `internal/reasoning` is the provider-neutral
reasoning vocabulary, about six hundred lines. `provider` already
declares the `ReasoningPolicy` interface; the vocabulary belongs
beside it.

## Goal

One composition package plans a session into a bounded provider
request, eliding and spooling what exceeds the window, with
calibrated estimates. The reasoning vocabulary folds into
`provider`.

## Scope

Inside:

- A `contextplan` package importing `contextstate`, `provider`, and
  `memory` only. `policy/layers.json` gains exactly that row.
- `Plan`, the core call: a session, a window budget, and an
  estimator in; a provider `Request`, a list of elision decisions,
  and spool references out.
- Elision policy: what drops first, what keeps a stub, what a
  retention class protects. Ported from the planner's rules.
- Calibration: the EWMA estimator wrapper over `TokenEstimator`,
  updated per completed turn.
- Reasoning redaction at checkpoint boundaries, ported from the
  source repo's policy hook.
- The `reasoning` fold: `provider` gains the vocabulary types
  behind `ReasoningPolicy`. No new package.

Outside:

- Durable state and ref minting. Phase 65 owns them.
- Spool storage. Phase 67 owns it; the planner only emits refs.
- Any concrete provider client. Callers stay the clients.
- Mivia's prompt templates and per-workflow context bindings.

## API

- `func Plan(ctx, sess *contextstate.Session, w Window, e provider.TokenEstimator) (PlanResult, error)`
- `type Window struct { MaxTokens int; Reserve int }` with `Validate`
- `type Elision struct { Ref contextstate.ContentRef; Reason string; Kept int }`
- `func Calibrate(est provider.TokenEstimator) *Calibrated`
- Provider-side: `provider.ReasoningVocabulary` types, locked in
  `api/provider.txt`.

## Tests

- Planner unit cases: fits whole, elides oldest first, respects
  retention, reserves headroom.
- Calibration: EWMA converges, bounds clamp, zero estimates skip.
- Checkpoint redaction: reasoning content never enters the active
  context.
- Property: the planned request never exceeds the window bound.

## Verification

- `make verify` passes with the new row in `policy/layers.json`.
- An e2e scenario wires `agentrun` over the planner: a long session
  completes with recorded elisions.
- `docs/plans/contextplan.md` lands with the code.
