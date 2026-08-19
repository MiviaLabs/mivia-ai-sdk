# Agent Instructions

Go SDK for building AI agents. Module:
`github.com/MiviaLabs/mivia-ai-sdk`.

## Layout

- `envelope/` — the message envelope (Message, Ack, Sign,
  VerifyThread). One package per concern; new concerns get new
  subpackages, never root-level Go files.
- `room/` — standing groups: membership roster, roles, admission.
- `machine/` — the status model: Status, Trigger, Guard, Transition,
  Fire, and the JSON wire form.
- `flow/` — the step graph, the sequential runner, and the parallel
  panel waves: Step, Panel, Definition, Run, Confirm, Outcome,
  Admission, Route, Failure, FailureFrom, RetryPolicy, LoopPolicy,
  LoopState, LoopStateFrom. A step named in
  a panel runs as part of that panel's wave, in a goroutine, once
  every member is ready. A step's Admission rule decides whether it
  runs once its needs are terminal, and the zero value is strict: a
  skipped need skips the step, so route exclusion propagates by
  default; a branch step's Route then picks
  which direct dependents the run keeps. A step's Retry field, when
  non-nil, retries its own Fire call with exponential backoff through
  RetryPolicy.NextDelay, up to MaxAttempts; New rejects Retry on a
  step with Sub or on a panel member. A step's Loop field, when
  non-nil, repeats its Sub child workflow, gated by LoopPolicy.Guard,
  before its own transition and Confirm fire; LoopPolicy.Max caps the
  iteration count, and zero means unbounded, bounded only by the
  caller's own ctx. Run injects a LoopState into ctx before each Guard
  call, readable through LoopStateFrom; New rejects Loop on a step
  with a nil Sub or on a panel member. A step admitted through a failed
  need (AdmissionOnFailed) is a fallback: it catches a dependency's
  Fire (including one that exhausted its retries or its loop) or Route
  failure and lets the run continue, reading the failed step's Failure
  through FailureFrom. Checkpoint, pause, and resume: Run's
  onCheckpoint hook, Checkpoint, Encode, Decode, and Resume let a
  caller pause a run by canceling ctx and resume it later from the
  last checkpoint; Checkpoint's Failed field preserves an
  already-caught failure's outcome across the pause, though a
  still-pending fallback's bookkeeping does not survive the round
  trip.
- `events/` — the in-process reaction bus. Caller-owned; no shared bus.
- `contextbudget/` — a leaf primitive: Limits, Validate, Fits. Limits
  caps one model call's context by byte count and event count, a zero
  field means no cap. Fits reports whether a candidate total stays
  under both caps. Imports no internal package; agent imports it for
  Run's optional budget check.
- `contextstate/` — the durable context contract and the canonical
  content-reference minter: HashPrefix, Digest, Mint, IsRef;
  ContentRef, PayloadRecord, Reassemble; SourceEvent, Checkpoint,
  Session; CommitRequest with NewCommitRequest and Validate; Limits;
  and a MemStore with New, Put, Get, Checkpoint, and Session. A leaf
  package; envelope imports it — ContextRef delegates to Mint, so
  every ref in this SDK has one form. Volume Limits are caller-owned
  and enforced at the store; a zero field means uncapped.
- `contextplan/` — fits one session into a bounded request: Planner,
  NewPlanner, Plan; Window with Validate, Budget, CompactTrigger, and
  CompactTarget; Compaction and the pure Compact with CompactResult,
  the structural retention half of compaction; PlanResult, Elision,
  ElisionReason; Calibrate and Calibrated, an EWMA-corrected
  provider.TokenEstimator safe for concurrent use; IsReasoningEvent;
  StubContent. Imports contextstate, provider, and memory. NewPlanner
  takes a contextstate.MemStore and a memory.Store; Plan drops or
  trims events to fit Window.Budget, applying StubContent to
  reasoning events first. Compact keeps the system message, the
  latest user objective, preserved names, and the latest complete
  assistant-plus-tool unit, then fills a contiguous tail; it fails
  ErrRetentionOverflow when the retention set alone exceeds the
  budget.
- `contextsummary/` — the LLM summarizer for compaction: Summary with
  Validate and Render, SummaryMessage, TokenEstimate, Summarizer over
  a provider.Completer. One call, a 20 second timeout, strict
  DisallowUnknownFields decoding, no retry. Bounded excerpts feed the
  prompt, newest first; a summary failure is caller-visible with no
  structural fallback. Imports provider only.
- `envfile/` — dotenv loading: Load and LoadBytes parse `KEY=VALUE`
  lines into a map without leaking parsed values into an error. Load
  reads the file and delegates to LoadBytes, so the two share one
  parser and one error set. A leaf package; no internal imports.
- `secretpath/` — glob-style secret path matching: Matcher, NewMatcher,
  Matches. Reports whether a path matches a configured pattern list.
  Matches on a nil Matcher returns false. A leaf package; no internal
  imports. `workspace` imports it for `Options.Deny`.
- `workspace/` — filesystem confinement: Workspace, Options, Validate,
  Open, OpenWith, Close, Root, ReadFile, ReadFileLimit, WriteFile,
  List, Stat, DefaultMaxReadBytes, Unbounded, ErrEscape, ErrTooLarge,
  ErrInvalidLimit, ErrSecretPath. Open binds a handle to a
  root directory through `os.Root`, so confinement runs at the syscall
  level, not through a path check the caller performs before a separate
  syscall. Every method rejects a path that escapes the root through
  traversal or a symlink. A read carries a byte bound: `Options{}`
  yields `DefaultMaxReadBytes`, and only `Unbounded` removes it, so an
  unset field fails closed. Close releases the held `os.Root`.
  `Options.Deny`, an optional `*secretpath.Matcher`, refuses a secret
  path with `ErrSecretPath` before the syscall: a name check on the
  cleaned root-relative path, then a symlink-component walk that
  closes the aliasing hole a name policy alone leaves open. A nil
  `Deny` denies nothing. Imports `secretpath`.
- `diff/` — bounded unified line diffs: Unified, ErrTooLarge. Unified
  computes a line-level diff and fails closed past a caller's line
  budget. A leaf package; no internal imports.
- `spool/` — a principal-scoped grant store for oversized content:
  Spool, NewSpool, Spool.Spool, Spool.Load, ContentStore,
  WithPrincipal, PrincipalFrom, SpoolTool. Spool.Spool writes content
  to a caller-supplied ContentStore, grants one principal the right
  to read it back, and returns a bounded view plus a reference.
  SpoolTool wraps a tools.Tool: an oversized string result spools
  instead of returning in full, and the wrapper forwards
  ExecutionProfile, MaxResultBytes, Privileged, and SchemaTool from
  the wrapped tool whenever it implements them. Imports tools only.

- `identity/` — the agent key wrap: Identity, New, Load, Sign,
  Signer. Imports envelope only.
- `heartbeat/` — liveness tracking: Monitor records a beat per id and
  reports ids that have gone silent past a fixed timeout.
- `discovery/` — capability cards: Card, Parse, Validate, Match. Leaf
  block; no internal imports.
- `a2a/` — the A2A v1.0 mapping: Part, Mapped, ToPart, FromPart.
  Imports envelope only. No network and no third-party import in
  phase 9; the a2a-go client is phase 10.
- `a2aclient/` — the a2a-go client adapter: Client, New, Close,
  TaskHandle, State, Send, Status, Result. Imports a2a and envelope.
  Sends one task, polls its status, and fetches its result over
  a2aproject/a2a-go's gRPC transport; re-verifies the signature after
  every remote hop. One of two packages allowed to import a2a-go and its
  google.golang.org/grpc dial dependency; a2aloopback is the other.
- `a2aloopback/` — the A2A loopback test fixture: `Loopback` starts a
  real gRPC A2A server on a loopback port for cross-package tests. No
  production code may import it, the same convention `durablefence`
  uses. Imports `a2a` and `envelope` internally, and
  `github.com/a2aproject/a2a-go`'s server-side packages plus
  `google.golang.org/grpc` externally, the same exception `a2aclient`
  carries, scoped to this package instead.
- `a2aack/` — the remote step ack: Options, Options.Validate, Remote,
  Wait, and sentinels. Turns a remote A2A task round trip into an
  `agent.AckWait`. This is an edge adapter, not an ordinary block: its
  one purpose is to adapt a remote transport to the composition layer,
  so it may import `agent` for the `AckWait` type, one of two
  exceptions to the rule that a block never imports the agent
  (`dispatch` is the other). Imports a2aclient, agent, and envelope.
  Carries no a2a-go import of its own; the loopback test fixture lives
  in a2aloopback.
- `dispatch/` — the NDJSON envelope endpoint: Handler, Options,
  Options.Validate, New, Endpoint, Endpoint.Handler, Send, SendResult,
  and sentinels. `Endpoint.Handler` answers POST requests whose body
  is newline-delimited envelope.Message JSON: it runs the receive
  ladder per line — Decode, VerifySignature, Room.Accepts, resolve,
  handle, NewAck, Confirm, Encode — each a fail-fast stage, and
  answers with one newline-delimited ack or JSON error object per
  line; the stream stays open across a per-line failure.
  `agent.EmitMessageDelivered`, called after VerifySignature, and
  `agent.EmitMessageAcked`, called after Confirm, are best-effort
  diagnostics outside the ladder; their error return never fails a
  line. `MessageDeliveredEvent` means "signature verified," not
  "room-admitted," since it fires before Room.Accepts. `Send` posts a
  batch of signed messages as one NDJSON request and parses the reply
  into one SendResult per line, in order. This is an edge adapter like
  `a2aack`: it may import `agent` for EmitMessageDelivered,
  EmitMessageAcked, and their event-name constants. Imports agent,
  envelope, events, and room. Stdlib-only; carries no third-party or
  a2a-go import.
- `tools/` — the tool registry: Tool, Registry, New, Add, Get, Remove,
  Run, RunScoped. SchemaTool, an optional interface a Tool implements
  to publish a parameter schema and decode raw argument bytes;
  SchemaOf reads it. Registry.Tools returns every registered Tool,
  sorted by name. A leaf package; no internal imports. agentloop's
  Definitions walks Registry.Tools and calls SchemaOf on each tool to
  build the offered tool set.
- `trigger/` — the shared "condition fired, so run this" vocabulary:
  Condition, Action, Registry, New, Add, Remove, Fire. A leaf package;
  no internal imports.
- `hooks/` — the named, multi-handler lifecycle-point registry:
  Point, PointPreTool, PointPostTool, PointStop, Handler, Registry,
  New, Add, Remove, Fire. Fire runs a point's handlers in
  registration order and stops at the first veto. A leaf package; no
  internal imports. No caller yet; the tools and flow wiring is a
  later phase.
- `trace/` — the structured-trace primitive: Span, SpanID, Tracer,
  New, Start, SpanFrom. Tracer.Start issues sequential SpanIDs and
  links each new span to the span already in ctx, following flow's
  LoopStateFrom pattern; End, SetAttribute, and Attributes are safe
  for concurrent use on one shared Span. A leaf package; no internal
  imports. No caller yet; the tools and trigger precedent.
- `skills/` — the reusable-instruction-bundle registry: Skill,
  Validate, Registry, New, Add, Get, Remove, Names, Match. A skill is
  read, not called: it carries instructions text, trigger phrases, and
  the tool names it expects available. A leaf package; no internal
  imports. No caller yet; the agent/subagent wiring is a later phase.
- `mcp/` — the MCP tool-calling client: Client, Connect, Transport,
  NewStdioTransport, NewStreamableHTTPTransport, ListTools, CallTool,
  and CallToolWithProgress. Imports tools internally and the official
  MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`) externally.
  Wraps the SDK's client over a local subprocess or a remote
  streamable HTTP endpoint and maps each remote tool into a
  tools.Tool a tools.Registry already knows how to hold and run.
- `ledger/` — the durable-task-admission primitive: idempotency-keyed
  admission, a leased claim with a monotonic fence, renewal, a stale
  takeover, and dependency-driven blocking on failure. Imports machine
  and events internally. `SQLiteStore`, behind the `ledger_sqlite`
  build tag, additionally imports the pure-Go `modernc.org/sqlite`
  driver externally; the default build never compiles it.
- `schema/` — the JSON Schema compile/validate/corrective-message
  primitive: Compiled, Compile, Validate, Corrective, MaxSchemaBytes,
  MaxSchemaDepth, MaxPayloadBytes, MaxCorrectiveBytes, ErrAdmission,
  ErrCompile, ErrMalformedPayload, and ErrValidation. Imports
  `github.com/santhosh-tekuri/jsonschema/v6` externally and no internal
  package.
- `agentloop/` — a second composition path beside flow: a tool-calling
  loop over a provider.Completer and a tools.Registry. Run offers the
  registry's schema-bearing tools, runs the model's requested calls
  through RunScoped, appends the results, and repeats until the model
  stops asking or a bound trips (MaxIterations, MaxCallsPerTurn,
  MaxTotalTokens, Budget, or ctx cancellation). A wired Hooks registry
  fires PointPreTool and PointPostTool per tool call, and PointStop
  once on every return path out of Run. Options.Window plans every
  iteration: Compact through the contextsummary Summarizer at the
  trigger, Observe after every Chat, and one prompt-too-long recovery
  retry under a one-percent trigger with a CompactionNotice.
  Imports provider, tools, trace, hooks, usage, events,
  contextbudget, schema, contextplan, and contextsummary. Never
  imports subagent; subagent imports agentloop starting in phase 70.
- `e2e/` — the end-to-end scenario harness: deterministic tools,
  an event recorder, and a thread-capturing resolver. The scenarios
  in `e2e/e2e_test/` wire real high-level blocks together and assert
  each run's outputs. See docs/plans/e2e.md.
- `subagent/` — the SDK's blocks as tools: `AsTool` wraps a built
  runner as a spawnable subagent tool, `RunAll` joins concurrent
  spawns behind a ctx depth guard, internal tools expose flow,
  ledger, memory, room, scheduler, heartbeat, discovery, provider,
  trigger, and channel blocks, and a signed-message `Mailbox` with
  `SendTool` and `InboxTool` carries both directions between
  orchestrators, subagents, and humans. Five file tools wire
  `workspace` and `diff` in: `WorkspaceReadTool`, `WorkspaceWriteTool`,
  `WorkspaceListTool`, `WorkspaceStatTool`, and `DiffTool`, each bound
  to a caller-supplied `*FileTools` at construction, never a
  model-chosen root. `FileToolOptions{Root, Deny, MaxReadBytes}` and
  `OpenFileTools` are the one place these five tools accept a
  workspace from; `Validate` rejects a nil `Deny`, returning
  `ErrDenyRequired`, before any filesystem call, so a caller cannot
  build a usable `*FileTools` with no secret policy. `(*FileTools)
  Close` closes the workspace `OpenFileTools` opened; the caller that
  opens it owns `Close`. `WorkspaceWriteTool` alone implements
  `tools.PrivilegedTool`. `ErrBadArguments` covers a bad decode or a
  mistyped `Run` call across the five. `workspace.ErrSecretPath` and
  `workspace.ErrEscape` propagate to the model unchanged. `envfile`
  gets no tool. See docs/plans/subagent.md.
- `durablefence/` — a leaf, test-only conformance kit for claim,
  takeover, and fence invariants. Imported only from another
  package's `_test` subdirectory, for example
  `ledger/ledger_test/scenario_test.go`.
- `memory/` — the shared context store: Store, New, Put, Get, keyed
  by `envelope.ContextRef`. A size budget evicts the oldest-inserted
  blobs. Imports envelope only.
- `provider/` — the model provider interface: Completer, RunTurn,
  Message, Request, Response, Chunk. Also carries the reasoning
  vocabulary: ReasoningEffort with its four levels, ReasoningPolicy,
  an optional Completer capability; ReasoningBlock, RedactBlock, and
  ReasoningEventKind, the contextstate.SourceEvent.Kind literal a
  reasoning trace carries. A leaf package; no internal imports. No
  concrete client ships in this SDK; a caller supplies its own
  `Completer`.
- `providerregistry/` — the named-provider collection and ordered
  fallback: Registry, Register, Get, Names, Retryable, Route. Route
  walks a caller-chosen order through provider.RunTurn and falls
  through to the next name only when the caller's Retryable predicate
  approves the failure. Imports provider only.
- `usage/` — the per-session running total of provider.Usage:
  Accumulator, New, Record, Total, Reset, WrapCompleter,
  ErrBlankSessionID, ErrNilAccumulator, ErrNilCompleter.
  Record sums one provider.Usage call onto the session's running
  total, guarded for concurrent use. WrapCompleter wraps a
  provider.Completer so every completed Chat turn records its usage
  under a session id; a streamed turn records nothing. Imports
  provider only.
- `channel/` — the ask-and-wait shape: Question, Answer, Notifier. A
  leaf package; no internal imports. Ships one reference transport,
  `NewNDJSONNotifier`, speaking newline-delimited JSON over an
  `io.Reader`/`io.Writer` pair; a caller builds any other transport
  itself and implements `Notifier` per real channel (terminal, Slack,
  another agent).
- `scheduler/` — the generic invoke-on-schedule primitive: Job,
  Schedule, Every, At, Scheduler, Add, Remove, Run. Fires each
  registered Job at its next scheduled time and emits JobFailedEvent
  on a caller-supplied bus when a Job errors. Imports events only.
- `agent/` — the composition layer: wires blocks into an agent.
- `agentrun/` — the config-struct composition layer over agent.Run:
  Options, New, ValidateMatrix, Artifacts, PayloadOf, and the
  hooks-and-tracer wiring.
- `taskrun/` — the ledger admit-claim-complete ceremony around one
  work func.
- `runconfig/` — a JSON document loader for `agentrun`: Load,
  Definition, Definition.Runner, Binding, Kind, Blocks, NewBlocks.
  Load resolves one JSON document into a validated Definition,
  feeding flow.New, machine.New, and agentrun.New. Runner builds the
  tool registry and returns an `*agentrun.Runner`. Imports agentrun,
  flow, machine, subagent, and tools.
- `api/` — exported-surface locks; `scripts/check_api.py` diffs them.
- `policy/layers.json` — allowed internal imports per package.
- `docs/plans/` — one plan per package; `scripts/check_plan.py` gates it.
- `docs/` — README.md is the index; architecture.md the single design
  reference (module map, message flow, wire rationale, gate system);
  packages/ the package references; examples/ the walkthroughs;
  plans/ the change contracts.
- `scripts/` — gates: check_docs, check_structure, check_deps,
  check_plan, check_prose, check_api, check_gomod,
  check_semgrepignore, check_semgrep_probes, check_labels,
  api_surface (Go).
- `semgrep/sdk-standards.yml` — pattern rules: no panic/exit in
  packages, stdlib-only imports, no string literals where constants
  exist (enums, hash prefix), wire bytes only via Encode, signing only
  via Sign, no hardcoded secrets, no suppression annotations, no
  unresolved-work markers. The Makefile suppression-marker scan rejects
  suppression markers in comments.
- `.semgrepignore` — lists only `.git/`, so Semgrep scans test files.
  scripts/check_semgrepignore.py pins its exact content.
- `.githooks/pre-commit` — runs `make verify-fast` on the staged
  snapshot; untracked files never enter the gate.
- `.agents/agents/` — subagent roles: planner, plan-reviewer,
  builder, reviewer. `.agents/skills/delivery/` drives the loop.
- `.agents/memories/` — team-shared, git-committed operational memory
  (corrected mistakes, standing preferences discovered while working in
  this repo), following the open `.agents` protocol
  (https://dotagentsprotocol.com/): one markdown file per memory, `id`/
  `title`/`content`/`importance`/`tags` frontmatter. Read every file here
  at the start of a task, the same way you read this file. Not a
  substitute for a rule enforced by a gate above — a fact that hardens
  into a rule belongs in this file or a gate script, not a memory file.
- `.agents/hooks.json` — PreToolUse lifecycle hooks for Antigravity,
  wiring `scripts/agent_hook_guard.py` across command and file tools.
- `.claude/` — aliases only: the agents and the delivery skill
  symlink to `.agents/`. `.claude/settings.json` stays here and wires
  PreToolUse hooks to `scripts/agent_hook_guard.py`. The guard blocks
  hook bypass, core.hooksPath overrides, and manual edits to generated
  `api/` locks and `.semgrepignore`. It covers Bash, Write, Edit,
  MultiEdit, and NotebookEdit.
- `CLAUDE.md`, `GEMINI.md` — thin adapters; import this file only.
- Root: no Go code. Root holds go.mod, README, this file, Makefile.

## Trigger words

The user's vocabulary is a contract. The `review` and `audit` triggers,
formerly defined here, now live in the `review` skill at
`.agents/skills/review/SKILL.md`. Invoke it for a deep review or a
gate-audit pass.

## Orchestrator role

The agent the user talks to is the orchestrator. It drives everything
else.

- Clarify first. Never start the delivery loop with ambiguity. Ask
  questions with proposals A, B, C. Mark the recommended option and
  say why in one sentence. Wait when the choice changes the design.
  Decide alone only when options are equivalent.
- Simplicity over complexity. Prefer the smallest change that works.
  Three files beat a framework. Reject planner output that adds
  abstraction without a caller. No speculative generality.
- Drive the loop below and consolidate the reports. The user gets one
  answer, not four.

## Building blocks

The SDK is a set of composable blocks, not a monolith. Every package
decision follows this rule.

- A package is a building block with one concern. A new concern gets a
  new top-level package, never a root file.
- Compose packages through their public API. Never copy a type into
  another package to dodge an import. Use the exported type.
- The import policy in `policy/layers.json` pins every edge. Direction
  flows inward: leaf blocks first, the composition last. The deps gate
  enforces it.
- An agent is the composition layer. It wires blocks: a transport
  adapter, a workflow runner, and the message plane. The agent imports
  the blocks; a block never imports the agent.
- Do not split a working package for purity alone. Split a package only
  when a real consumer needs the concern by itself. Keep cohesion. The
  building-block rule is about composing behaviors, not about tearing
  one cohesive struct into many packages: `envelope` holds the
  message, the ack, the signing, and the thread chain in one package
  because the four concerns share one struct and cannot split without
  artificial layering.
- A block stays replaceable and testable on its own. Do not entangle it
  with a caller.

## Subagent workflow

Non-trivial changes (new package, API change, more than one file) go
through the delivery loop in `.agents/skills/delivery/SKILL.md`:
planner → plan-reviewer (hostile, before code) → builder → reviewer
(adversarial, after code) → verify → commit. Never skip a review
stage. Never let an agent grade its own work. Three failed rounds at
any stage means stop and escalate to the user.

## Rules

- **Writing standard (critical):** all agent-authored prose (plans,
  docs, comments, commit messages, reports) uses ASD-STE100-style
  Simplified Technical English. One idea per sentence. Sentences stay
  at or below 25 words. Instructions use the imperative mood. Same
  thing, same word — no synonym drift. No filler words ("simply",
  "just", "seamless", "robust"). Gate: `scripts/check_prose.py`
  enforces sentence length in `docs/plans/`.
- Run `make install-hooks` once per clone, `make verify` before you
  report done. `make verify` is the full gate: gofmt, vet, tests,
  doc gate, structure gate, Semgrep scan, and probes.
- Never bypass Git hooks (no `--no-verify`, no skip env vars).
- The GitHub remote for this repo must be **private**. Never create a
  public remote or push to one.
- No third-party dependencies. Standard library only.
  Exception: `a2aclient` may import `github.com/a2aproject/a2a-go` and
  `google.golang.org/grpc`; `a2aloopback` may import the same two
  modules, scoped to its own gRPC test-server fixture; `mcp` may import
  `github.com/modelcontextprotocol/go-sdk`; `ledger` may import
  `modernc.org/sqlite`, behind the `ledger_sqlite` build tag only;
  `schema` may import `github.com/santhosh-tekuri/jsonschema/v6`; no
  other package may add a third-party import without its own plan
  review.
- Comments are a machine-read API surface. Keep them short: one line of
  what, plus invariants and cross-references (`See X`, file names) where
  they exist. No prose paragraphs, no restating the signature.
- Every exported symbol needs a doc comment starting with the symbol
  name. Enforced by `scripts/check_docs.py`.
- Files stay at or below 500 lines, functions at or below 80 lines.
  Enforced by `scripts/check_structure.py`. Split code; do not raise
  the limits.
- Invariants live in `Validate` methods, not in comments alone. If a
  comment states a rule, `Validate` must enforce it.
- File and function names must describe the feature, not the development
  process. Forbidden in names: phase, tdd, perf, wip, draft, scratch,
  tmp, old, backup, version suffixes (v2, v3). Use descriptive names
  like `panel_test.go`, `chain_bench_test.go`. Gate:
  `scripts/check_names.py`; Semgrep:
  `sdk.go.no-phase-tdd-perf-names`. Plan documents in
  `docs/plans/agents/` may use phase numbers as plan identifiers.
- No string literals where constants exist: enum values (Intent,
  Epistemic, AckStatus, Role), hash prefixes, wire serialization
  (Encode), signing (Sign). Enforced by `semgrep/sdk-standards.yml`.
  Tests may construct invalid values on purpose.
- Do not suppress Semgrep findings with inline annotations; fix the
  rule or the code.
- No audit-finding labels in comments, docs, or plans. A label is a
  letter A through G followed by a digit. Gate:
  `scripts/check_labels.py`.
- Tests table-driven where the case set grows. Test the invariants that
  `Validate` claims to enforce.
- The wire contract is pinned by conformance vectors in
  `envelope/testdata/vectors/`. Add a vector for every schema or rule
  change: `valid_`, `invalid_decode_`, or `invalid_sig_` prefix.
- Changes to message semantics must update `docs/architecture.md`'s
  "Why the envelope is shaped this way" section in the same change.

## Enforcement ladder (all mechanical, all in make verify)

Rules below are phrased as prohibitions because that is what agents
follow reliably. Each has a gate behind it.

- Do not add or change an exported symbol without a deliberate lock
  update: `make api-update`, then commit the `api/` diff in the same
  change. Gate: `scripts/check_api.py`.
- Do not import another package of this module unless
  `policy/layers.json` allows the edge. A new package must declare its
  allowed imports there first. Gate: `scripts/check_deps.py`.
- Do not copy an exported type into another package to reuse it. Import
  the source package; the import policy already allows the edge. A
  copied type forks on the next change. Gate: review catches the copy.
- Do not let a package see its own caller. Dependency direction flows
  inward; the import policy declares each edge, so a cycle or a caller
  import cannot compile. Gate: `scripts/check_deps.py`.
- Do not land a package without `docs/plans/<pkg>.md` following
  `docs/plans/TEMPLATE.md` (Goal, Scope, API, Tests, Verification).
  Gate: `scripts/check_plan.py`.
- Do not let coverage fall below 85%. The total and every package each
  need the floor. Gate: `make verify` coverage block. Assertion-free
  tests and deleted tests game the floor; review catches them.
  Mutation testing is future work.
- Do not write an audit-finding label in comments, docs, or plans: a
  letter A through G followed by a digit. Gate:
  `scripts/check_labels.py`.
- Do not weaken a gate, raise a limit, or widen an exclusion to make
  your change pass. Change the design instead, or convince the user
  and record the exception in the gate file itself.

## Gate tiers

Two tiers guard the tree. `make verify-fast` runs the fast local
checks: gofmt, vet, tests, the python gates, the Semgrep scan, and the
suppression-marker scan. The pre-commit hook runs `make verify-fast` on
the staged snapshot.

`make verify` runs `verify-fast`, the coverage floor block, and the
Semgrep probe suite. The probes prove every Semgrep rule fires on a
violation and stays silent on clean code. The coverage block asserts
the profile lists every package and that the total and each package
reach 85.

The hook guard and the pre-commit hook are best-effort against
careless agents. They are not a security boundary. GitHub Actions CI
now runs `make verify` on every push and pull request to `main`. No
branch protection rule exists yet, so CI stays informational only: a
failing check does not block a merge or a direct push.
