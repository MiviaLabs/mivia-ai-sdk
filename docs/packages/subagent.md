# Package reference: subagent

The subagent package exposes the SDK's blocks as tools. An
orchestrator runner spawns subordinates from its own registry, runs
several at once, observes their runs, and exchanges signed messages
with them. The exported surface below mirrors `api/subagent.txt`.

## Spawn and join

- `AsTool(name, r, opts)` — one built runner as a tool. Each call
  drives one full run on a fresh thread. `opts.Artifact` names the
  artifact returned, read from `opts.Artifacts`; without one the
  final status returns. `opts.Bus` receives the spawned run's agent
  events. `opts.Tracer` opens one `subagent.spawn` span per spawn, carrying
the spawn's thread. `opts.Depth` bounds recursive spawns; zero means
three.
- `RunAll(ctx, specs)` — every spec's runner runs concurrently; one
  result per spec, in spec order. One member's error never cancels
  its siblings.
- `ErrMaxDepth` — a spawn past the bound. The depth rides the ctx,
  so nested spawns count every level.

A flow panel cannot spawn tools: a wave never reaches the ack chain
where tools run. Parallel subagents fan out through `RunAll` inside
one step's tool.

## Internal tools

Every tool takes the string payload the ack chain delivers and
returns a string. Command tools decode JSON payloads and map any
decode or dispatch fault onto `ErrBadCommand`.

- `FlowTool` — runs a flow plan once and returns the final status; a
  bound bus observes one `StepCompletedEvent` per step.
- `LedgerTool` — `OpRun` wraps the full taskrun ceremony around a
  no-op work function, landing the key completed; a blocked or
  replayed key fails with the ceremony's own sentinel. `OpState`
  reports a key's status, or `absent`.
- `MemoryTool` — `OpPut` stores data and returns its
  content-addressed ref; `OpGet` returns the bytes.
- `RoomTool` — admit, remove, promote, members, and ismember
  against a bound room, acting as the bound actor unless the
  command names one.
- `SchedulerTool` — schedules one bound job on an interval or at
  fixed times, and cancels by id.
- `HeartbeatTool` — beat, alive, and dead against a bound monitor.
- `DiscoveryTool` — parses one capability card per call and reports
  the matching capability, or `none`.
- `ProviderTool` — one model turn through a caller-supplied
  `Completer`; the prompt in, the reply content out.
- `ProviderRegistryTool` — one turn routed over the named-provider
  fallback order; the prompt in, the answering provider's reply out.
- `TriggerTool` — fires a named trigger; the registry's own action
  runs.
- `ChannelTool` — asks one human through a `Notifier`; an approved
  answer's payload returns, a decline fails naming the recipient.

## File tools

Five tools wire the leaf packages `workspace` and `diff` into a
model-reachable tool set. Each binds one `*FileTools` a caller opens
once, at wiring time; a model never chooses the sandbox root or the
secret-path deny list, only a path inside the root. Each implements
`tools.SchemaTool`, so `agentloop.Definitions` offers it without a
caller-written adapter.

`FileToolOptions{Root, Deny, MaxReadBytes}` and
`OpenFileTools(opts) (*FileTools, error)` are the one place these
five tools accept a workspace from. `Validate` rejects a nil `Deny`
before any filesystem call, wrapped in `ErrDenyRequired`: a caller
cannot build a usable `*FileTools` without naming a secret policy,
even an empty one (`secretpath.NewMatcher(nil)`). `(*FileTools)
Close()` closes the workspace `OpenFileTools` opened; the caller that
opens a `FileTools` owns the matching `Close`.

- `WorkspaceReadTool(name, ft, maxResultBytes)` — reads one file,
  relative to `ft`'s bound root. A positive `maxResultBytes` publishes
  `tools.ResultBudgetTool`; a non-positive value publishes no budget.
  Not privileged.
- `WorkspaceWriteTool(name, ft)` — writes one file, relative to
  `ft`'s bound root, at a fixed `0o600` mode. Implements
  `tools.PrivilegedTool`, so `tools.Scope.Allowed` denies it unless a
  caller's `Allowlist` names it.
- `WorkspaceListTool(name, ft)` — lists one directory, relative to
  `ft`'s bound root. Result is a JSON-encoded string of
  `[]WorkspaceEntry`, not the raw typed struct. A blank path lists the
  root. Not privileged.
- `WorkspaceStatTool(name, ft)` — stats one path, relative to `ft`'s
  bound root. Result is a JSON-encoded string of one
  `WorkspaceFileInfo`, not the raw typed struct. Not privileged.
- `DiffTool(name, ft, maxLines)` — diffs the on-disk content at a
  path, read through `ft`'s bound workspace, against proposed content,
  through `diff.Unified`. A not-yet-existing path diffs against empty
  content. A diff over `maxLines` fails with `diff.ErrTooLarge`. Not
  privileged.

`workspace.ErrSecretPath` and `workspace.ErrEscape` propagate to the
model unchanged from any of the five tools; see
`docs/plans/agents/phase71_filetools.md` for why no translation layer
sits between them.

Every argument struct (`WorkspaceReadArgs`, `WorkspaceWriteArgs`,
`WorkspaceListArgs`, `WorkspaceStatArgs`, `DiffArgs`) carries `json`
tags and decodes through the tool's own `DecodeArguments`. `ws`'s own
`resolve` step rejects a traversal or an absolute path with
`workspace.ErrEscape`, whatever a caller's schema or decode step lets
through. `envfile` gets no tool: a parsed dotenv map is a
credential-exfiltration primitive with no matching model-facing
benefit.

## Command wire

Command tools decode one JSON payload struct per call: `RoomCommand`,
`SchedulerCommand`, `HeartbeatCommand`, `LedgerCommand`,
`MemoryCommand`, and `DiscoveryCommand`. Each names its operation
with an `Op` constant: `OpAdmit`, `OpRemove`, `OpPromote`,
`OpMembers`, `OpIsMember`, `OpEvery`, `OpAt`, `OpCancel`, `OpBeat`,
`OpAlive`, `OpDead`, `OpRun`, `OpState`, `OpPut`, `OpGet`, and
`OpMatch`.

## Message plane

- `NewMailbox(capacity)` — a bounded inbox of signed messages for
  one recipient. `Deliver` validates and appends, failing with
  `ErrMailboxFull` at the bound; `Take` drains in delivery order.
- `SendTool(name, box, id)` — signs one message per call with the
  caller's identity and delivers it. Any sender uses the same
  surface: an orchestrator step, a sibling subagent, or human
  wiring.
- `InboxTool(name, box)` — drains the mailbox and returns its
  payloads comma-joined, or `empty`.

The room pattern: an orchestrator admits the subagent's signer
through `RoomTool` before delegating; membership and messaging stay
separate planes, with `dispatch` carrying room messages over HTTP.

## Invariants

- A subagent tool runs in-process under the parent's process; the
  remote boundaries stay with `a2aack` and `dispatch`.
- The depth guard bounds recursion; the default is three.
- Event forwarding never removes a handler: the runner's bus keeps
  every forwarder for its lifetime.
- Every spawn runs on a fresh thread; repeated spawns mint new
  thread names, never reusing one.

## Failure modes

- `ErrMaxDepth` ("subagent: max spawn depth reached") — the spawn tool
  wraps it when the current depth is at or over `opts.Depth`. Pinned by
  `subagent/subagent_test/depth_test.go` (`errors.Is`).
- `ErrBadCommand` ("subagent: bad command") — the command tools
  (`MemoryTool`, `RoomTool`, `LedgerTool`, `SchedulerTool`,
  `HeartbeatTool`, `DiscoveryTool`) wrap it for malformed JSON or an
  unknown `Op`. Pinned by
  `subagent/subagent_test/toolerrors_test.go`.
- `ErrMailboxFull` ("subagent: mailbox is full") — `Mailbox.Deliver`
  returns it when the mailbox is already at capacity. Pinned by
  `subagent/subagent_test/mailbox_test.go`.
- `ErrInvalidCapacity` ("subagent: mailbox capacity must be positive")
  — `NewMailbox` wraps it, with the offending capacity in the message,
  when `capacity` is not positive. Pinned by
  `TestNewMailboxRejectsBadCapacity` in
  `subagent/subagent_test/mailbox_test.go` with `errors.Is`, for both
  a zero and a negative capacity.
- `ErrBadArguments` ("subagent: bad arguments") — the five file tools
  wrap it when `DecodeArguments` cannot parse raw bytes, or when
  `Run`'s `tools.InOut.Value` does not carry the tool's typed argument
  struct. Pinned by
  `subagent/subagent_test/filetools_test.go`.

## Usage

```go
subBox, _ := subagent.NewMailbox(8)
reg := tools.New()
reg.Add(subagent.SendTool("greet", subBox, parentID))
reg.Add(subagent.AsTool("delegate", subRunner, subagent.ToolOptions{
    Artifact: "inbox", Artifacts: subArtifacts, Bus: parentBus,
}))
reg.Add(subagent.InboxTool("collect", parentBox))
```

See [../plans/subagent.md](../plans/subagent.md) and the system
scenarios in [../plans/e2e.md](../plans/e2e.md).
