# Phase 45: agent composition example

Status: future. Plan-only; it has not yet gone through plan review.
Depends only on already-shipped packages: `provider` (phase 29),
`tools` (phase 14, extended in phase 31 and phase 36), `mcp`, `ledger`
(phase 34), `memory` (phase 15), and `agent` itself. Every dependency
is shipped, gate-clean, and covered by its own plan. This phase adds
no code to any of them.

## Why this phase exists

A completeness review found that `agent`, the package AGENTS.md names
as the composition layer, imports 8 of the SDK's 21 packages today:
`identity`, `discovery`, `flow`, `envelope`, `events`, `machine`,
`heartbeat`, `contextbudget`. Five shipped, tested packages —
`provider`, `tools`, `mcp`, `ledger`, `memory` — have no caller wiring
them into `agent.Run`. This phase investigates whether that gap is a
missing import edge or a missing demonstration, and finds the latter.

## The investigation

`agent.Run` (`agent/run.go`) builds each gated step's
`envelope.Message` from `step.Payload`, a plain `string` already bound
into the `flow.Definition` `agent.New` received. `confirmStep` signs
that message and calls `wait` (`AckWait`) only to resolve the step's
ack; `wait`'s return value is an `envelope.Ack`, never a payload, so
nothing `wait` returns can change the message `Run` already built and
signed. This fixes the two seams a caller has:

- Anything that shapes a step's content must run before `flow.New`
  builds the plan and before `agent.New` binds it — at plan-
  construction time, entirely outside `Run`.
- Anything that reacts to a delivered step — running a tool, claiming
  durable ownership, reading or writing shared context — runs inside
  the caller-supplied `AckWait` closure, or around the `Run` call
  itself, entirely outside `agent`'s own code.

Checked against those two seams, each of the five packages already
composes with `agent.Run` with zero new import:

- **`provider`**. A caller calls `provider.RunTurn` (or `Chat`
  directly) against a `Completer` when building a step's `Payload`
  string, before `flow.New`. `provider.md`'s own text speculates about
  a future `Completer` field on `Agent`; that speculation does not
  match `Run`'s real shape, where `Payload` is fixed before `Run`
  starts and `wait` has no way to feed generated content back into an
  already-signed message. Plan-time generation is the only place a
  model turn's output can land.
- **`tools`**. An `AckWait` implementation reads `msg.Payload`, decides
  what tool call it names, and calls `Registry.RunScoped(ctx, name,
  in, scope)` before it builds the returned `envelope.Ack`. `AckWait`'s
  existing signature, `func(ctx, envelope.Message) (envelope.Ack,
  error)`, already gives a caller everything `RunScoped` needs: a
  `ctx` and the full signed message. `tools.md`'s own text names a
  "future agent-binding caller" for its capability and approval
  metadata; `AckWait` is that caller, already shipped.
- **`mcp`**. `mcp.RegisterAll` and `mcp.Client.ListTools` already map
  an MCP server's tools onto `tools.Tool` and add them to a
  `tools.Registry` (`mcp`'s only internal import is `tools`, per
  `policy/layers.json`). A caller who wants MCP-sourced tools inside
  an `AckWait` closure calls `mcp.RegisterAll` once, against the same
  `Registry` that closure already holds, before `Run` starts. `agent`
  never needs to import `mcp`; `mcp.md`'s own text already defers
  "wiring `mcp` into `agent`" to a later phase, and this investigation
  finds that later phase is unnecessary.
- **`ledger`**. `ledger.md`'s own text states the composition already:
  "A `flow.Run` invocation can itself be the task body a `ledger`
  owner claims and executes; the two compose, neither wraps the
  other." The same holds one level up: a caller calls `ledger.Claim`,
  runs `agent.Run` as the claimed task's body, then calls
  `ledger.Complete` with the resulting status. This is the same
  wrap-`Run`-in-a-closure shape phase 37's `channel.Notifier` plan
  already used for `scheduler.Job` and `trigger.Action`.
- **`memory`**. `memory.Store.Put` and `Get` work on content-addressed
  `[]byte` blobs referenced by an `envelope.ContextRef` string. A
  caller puts shared context into a `Store` at plan-construction time
  and threads the returned ref into a `Step.Payload`, or puts a tool's
  result into the `Store` inside `AckWait` after `RunScoped` returns.
  Because `Payload` is fixed before `Run` starts, `memory` needs no
  `Run`-time hook, the same reasoning that places `provider` at plan-
  construction time.

`scheduler`, `trigger`, and `channel` stay out of scope for the same
reason phase 37's `channel` plan already gave: `scheduler.Job` and
`trigger.Action` are `func(ctx) error` closures a caller wraps around
`agent.Run` directly, and `channel.Notifier` is one implementation
shape for the closures `AckWait` and `ErrEscalated` already expect.
None of the three needs a new `agent` import; this phase does not
revisit them.

## Conclusion

`agent`'s row in `policy/layers.json` does not change. `agent/run.go`
and every other file in `agent/` stay untouched. No exported symbol is
added, changed, or removed anywhere in the SDK. The gap this phase
closes is a demonstration gap: nothing today shows a caller composing
`agent.Run` with `provider`, `tools`, `mcp`, `ledger`, and `memory`
through the seams above, in one runnable program.

## Goal

Give the SDK one runnable, worked example that composes `agent.Run`
with `provider`, `tools`, `ledger`, and `memory` through the seams the
investigation above names, and a documented (non-runnable) paragraph
showing where `mcp` slots into the same `tools.Registry` with zero
extra import. A reader copies a working pattern instead of guessing at
wiring five packages by hand.

## Scope

Inside:

- A new walkthrough, `docs/examples/agent-composition.md`, with one
  complete fenced Go program, following the exact format of
  `docs/examples/agent-dispatch.md` (a `## The program` fenced block,
  a `## The call order` Mermaid sequence diagram, a `## What the
  program shows` prose section).
- A new `## Composing with provider, tools, mcp, ledger, and memory`
  section in `docs/packages/agent.md`, cross-referencing the seams
  above and the new example. Prose only; no invariant changes, because
  `Run`'s contract does not change.
- One new bullet in `docs/architecture.md`'s message-flow or agent
  section, stating that `provider`, `tools`, `mcp`, `ledger`, and
  `memory` compose around `Run` through `AckWait` and plan
  construction, not through a direct import edge. The `flowchart`'s
  `agent -->` edges stay exactly as drawn today; this phase adds no
  new arrow, because no new import exists.
- One new bullet in `docs/README.md`'s Examples list, linking the new
  file.

Outside:

- Any Go code change, in `agent/` or any other package. No file under
  a `.go` extension changes in this phase.
- Any `api/*.txt` lock change. `make api-update` is not run; no
  exported symbol changes anywhere.
- Any `policy/layers.json` change. `agent`'s row, and every other row,
  stays exactly as it is today.
- A live MCP server transport inside the example. `mcp`'s own
  conformance tests (`mcp/client_test.go`, `mcp/connect_test.go`)
  already prove `RegisterAll`'s mapping onto `tools.Tool`; this phase
  cites that proof in prose instead of standing up a stdio or HTTP
  server inside a documentation walkthrough. Spinning up a transport
  for one illustrative registration call is unjustified weight for a
  fact already proven elsewhere.
- Any change to `scheduler`, `trigger`, or `channel`. Phase 37 already
  answered their composition; this phase does not reopen it.
- Any change to `AGENTS.md`'s Layout section. No package gains a new
  file, export, or import; the `agent/` bullet's claims stay accurate
  unchanged.

## API

No exported Go symbol is added, changed, or removed. `api/agent.txt`
and every other `api/*.txt` lock stay byte-identical. `make
api-update` is not run in this phase.

## Tests

This phase ships no Go test file; it ships one runnable example
program inside a Markdown fence, verified the way every file under
`docs/examples/` is verified (see `.claude/skills/docs-maintenance`'s
"Example correctness" rule): extract the fenced code, build and run it
against the real module, and confirm its printed output matches the
"What the program shows" section exactly.

The example program must prove, in order:

1. A `memory.Store`, built with `memory.New`, holds one context blob
   (a short customer record) `Put` before the plan is built; the
   returned ref is a `string` from `envelope.ContextRef`.
2. A local `provider.Completer` test double (a package-local type
   implementing `Name`, `Chat`, and `ChatStream`) returns a canned
   `provider.Response` whose `Message.Content` embeds that ref,
   standing in for a model that read retrieved context and drafted a
   step's text. `provider.RunTurn` drives the call.
3. The drafted content becomes one `flow.Step`'s `Payload`, built
   before `flow.New` and `agent.New` run — proving where a model-
   generated payload actually plugs in: plan-construction time, not
   inside `Run`.
4. A `tools.Registry` holds one locally defined `tools.Tool` (a review
   tool). One paragraph in `## What the program shows`, not runnable
   code, states that `mcp.RegisterAll(ctx, client, reg)` adds MCP-
   sourced tools to the same `Registry` before `Run` starts, citing
   `mcp/client_test.go`'s round-trip proof instead of running a server
   in the example.
5. A `ledger.Ledger`, built over `ledger.NewMemStore()` and an
   `events.Bus`, admits the task with `Admit`, then claims it with
   `Claim` before `agent.Run` runs and completes it with `Complete`
   after `Run` returns, matching `ledger.md`'s own "task body" framing.
6. The `AckWait` closure reads the signed step message's `Payload`,
   calls `reg.RunScoped(ctx, "review", tools.InOut{Value: payload},
   nil)`, puts the tool's result back into the `memory.Store` with a
   second `Put`, and returns a confirmed `envelope.Ack` built through
   `envelope.NewAck(...).Confirm()`, carrying the tool's result as the
   ack's comment.
7. `agent.Run` runs with its existing nine positional arguments,
   unchanged from `docs/examples/agent-dispatch.md`'s call shape. The
   program prints the final `machine.Status`, the ledger's post-run
   `TaskState`, and the memory ref the tool result was stored under.

## Verification

- `python3 scripts/check_prose.py`, `check_labels.py`, `check_docs.py`,
  and `check_structure.py` all pass over the new and edited files.
- `python3 scripts/check_plan.py` passes; this phase adds no new
  top-level Go package, so it needs no `docs/plans/<pkg>.md` entry
  beyond this phase plan itself.
- `python3 scripts/check_deps.py` passes; `policy/layers.json` is
  unchanged, so there is no new edge to validate.
- `make verify` passes. No Go file changed, so the gofmt, vet, test,
  Semgrep, and coverage blocks run unaffected and stay green.
- The example program in `docs/examples/agent-composition.md` is
  extracted and run (`go run`) against the real module before this
  phase is called done; its printed output matches the "What the
  program shows" prose exactly, per the docs-maintenance skill's
  example-correctness rule.
- `docs/README.md`'s Examples list gains the one new bullet in the
  same change as the new file.
- `docs/plans/agents/PHASES.md`'s phase order gains this phase, marked
  plan-only until it passes plan review.
