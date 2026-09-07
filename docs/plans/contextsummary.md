# Plan: contextsummary

Status: shipped. Ports the summarizer half of the sibling consumer
repo's `internal/contextmgr`, simplified to this task's contract.
Compaction is LLM-only; this package is the only summarizer.

## Goal

Turn the messages a compaction drops into one validated, bounded
`Summary` document, through one bounded `provider.Completer` call. A
summary failure is a caller-visible error. No structural fallback
exists.

## Scope

Inside:

- `Summary`, a data-only document with bounded fields.
- `Summarizer`, backed by one `provider.Completer`. One call, one
  20-second timeout, strict JSON reply parsing, no retry.
- `SummaryMessage`, the injection helper. It renders a `Summary` as
  one named user-role message that compaction preserves through
  `PreserveNames`.
- Token pricing for summary bytes: `TokenEstimate`, bytes divided by
  four. `TokenEstimate` names its caller: an external budget planner
  that prices summary bytes against a window budget. No in-tree
  package calls it today; it stays because the pricing rule is part
  of the summary contract, and dropping it would push every caller to
  re-derive the divide-by-four rule.

Outside:

- Any retention or trigger math. `contextplan` owns compaction; see
  `docs/plans/contextplan.md`.
- Any loop wiring. `agentloop` owns the compaction call sequence; see
  `docs/plans/agentloop.md`.
- Any degrade-to-structural path. A summarizer failure fails the
  caller. This reverses the reference design, which fell back to
  structural-only compaction.
- Output-token caps through `provider.Request`. `Request` carries no
  `MaxTokens` field, and this plan adds none. The bound is the bounded
  input, the timeout, and strict output validation.
- Policy snapshots, binding revisions, endpoint allowlists, and
  redaction classifiers. Those reference mechanisms are out of scope.
- Any durable storage of summaries. The caller keeps the injected
  message in its own history.

## API

The surface below is the lock target. It lands in
`api/contextsummary.txt` via `make api-update`.

```go
// MaxFieldBytes bounds every individual Summary text field and every
// list item.
const MaxFieldBytes = 2 * 1024

// MaxItems bounds every Summary list.
const MaxItems = 32

// MaxExcerptTotalBytes bounds the whole source-excerpt section of one
// summarize prompt.
const MaxExcerptTotalBytes = 16 * 1024

// SummaryTimeout bounds one summarize call.
const SummaryTimeout = 20 * time.Second

// SummaryMessageName is the provider.Message.Name of the injected
// summary message. Compaction preserves it through PreserveNames.
const SummaryMessageName = "context-summary"

// Summary is one validated summary document. Data only: no tool,
// policy, or credential fields.
type Summary struct {
    Objective string
    State     string
    Decisions []string
    OpenWork  []string
    Risks     []string
}

// Validate enforces every bound this package claims: valid UTF-8, no
// control characters, non-empty Objective and State, MaxFieldBytes per
// field and per item, at most MaxItems per list, no duplicate items,
// and no blank item (empty or whitespace-only).
func (s Summary) Validate() error

// TokenEstimate prices n bytes at n/4 tokens, minimum one for
// non-zero input, zero for zero input.
func TokenEstimate(n int) int

// Summarizer adapts one provider.Completer to summary generation.
type Summarizer struct { /* unexported fields */ }

// NewSummarizer binds one Completer. A nil Completer wraps
// ErrNilCompleter.
func NewSummarizer(c provider.Completer) (*Summarizer, error)

// Summarize makes one bounded Completer call over msgs and returns the
// validated Summary. Never retries. Any failure is caller-visible.
func (s *Summarizer) Summarize(ctx context.Context, msgs []provider.Message) (Summary, error)

// Render returns the deterministic text form of s: one labeled line
// or bullet per field, in field order.
func (s Summary) Render() string

// SummaryMessage renders s as one RoleUser message named
// SummaryMessageName, whose Content is s.Render().
func SummaryMessage(s Summary) provider.Message

// Sentinel errors; test with errors.Is.
var (
    ErrNilCompleter = errors.New("contextsummary: completer is required")
    ErrNoMessages   = errors.New("contextsummary: no messages to summarize")
    ErrInvalidReply = errors.New("contextsummary: reply failed strict parsing or validation")
    ErrCallFailed   = errors.New("contextsummary: summary call failed")
)
```

### Decisions

- The summary request carries no `MaxTokens`. `provider.Request` has
  no such field today. Adding one for this single caller would change
  every `Completer` implementation for a bound the SDK cannot enforce.
  The call is bounded three ways instead: excerpts cap the input at
  `MaxExcerptTotalBytes`, `SummaryTimeout` caps the duration, and
  strict parsing plus `Summary.Validate` cap the accepted output. An
  over-long or malformed reply fails with `ErrInvalidReply` and no
  retry. This matches the fail-closed rule the task states.
- The prompt is two messages. A system message states the task and the
  exact JSON reply schema. A user message carries the bounded
  excerpts, newest first, each capped at `MaxFieldBytes`.
- The reply decode accepts at most one markdown code fence, rejects
  unknown fields, empty replies, and trailing bytes, through
  `encoding/json` with `DisallowUnknownFields`.
- `Summarize` never mutates `msgs` and never reads `Request.Tools`.
  The call sets `Stream` false and leaves `Model` empty, so the
  Completer uses its own default model.
- The injected message rides as `RoleUser` with `Name` set. This needs
  the `provider.Message.Name` field; see `docs/plans/provider.md`.
- `provider.Message.Validate` must accept the injected message. The
  Name rules there allow a name on `RoleUser`; this package relies on
  that rule.

## Tests

`contextsummary/contextsummary_test/`, one external test package.

- `summary_test.go` — table-driven over `Summary.Validate`: every
  bound claimed. Oversized field, oversized item, empty objective,
  empty state, over-full list, duplicate items, a blank item in each
  list (an empty string and a whitespace-only string), control
  characters, invalid UTF-8, and every valid shape.
- `render_test.go` — `Render` is deterministic; equal summaries render
  equal text; every field appears; `SummaryMessage` returns
  `RoleUser`, `SummaryMessageName`, and `Render`'s text; the message
  passes `provider.Message.Validate`.
- `token_estimate_test.go` — zero maps zero; one to four bytes map to
  one token; exact multiples divide cleanly.
- `summarizer_test.go` — a scripted `Completer` in the test package:
  - Happy path: one call, reply JSON decoded, fields validated, prompt
    excerpts newest first and byte-capped.
  - Nil Completer fails `NewSummarizer` with `ErrNilCompleter`.
  - Empty `msgs` fails with `ErrNoMessages`, and no Completer call
    runs.
  - Completer error wraps `ErrCallFailed`; exactly one call ran.
  - Malformed JSON, fenced JSON, unknown fields, trailing bytes, and
    an over-bound reply each fail with `ErrInvalidReply`.
  - A slow Completer under a canceled or timed-out ctx fails; the
    caller ctx stays the outer authority.
  - The 20-second cap applies: a `Summarizer` never runs past
    `SummaryTimeout` even when ctx has no deadline.
  - No retry: a Completer that fails once is called exactly once.
- `excerpt_test.go` — a long message list truncates per item and in
  total; the prompt stays under `MaxExcerptTotalBytes` plus the fixed
  prompt text.

Every scripted `Completer` lives in the test package. No concrete
client ships here.

## Verification

- `make verify` passes with the new `contextsummary` row in
  `policy/layers.json`, present before any code lands.
- `api/contextsummary.txt` lands via `make api-update`, matching the
  API section above.
- `go test -race ./contextsummary/...` passes.
- Coverage floor of 85 holds for `contextsummary` and the total.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass.
- Same-change doc work: `docs/packages/contextsummary.md`,
  a `docs/README.md` index entry, a `docs/architecture.md` module-map
  bullet, and an `AGENTS.md` Layout line for `contextsummary/`.
- This package lands with or after the `provider` change that adds
  `Message.Name`; it does not compile before that field exists.

## Correctness fix: duplicate detection keys on the raw item

Status: shipped in commit f23b6e9.

### Fix goal

`Summary.Validate`'s doc comment claims "no duplicate items". Its
worker, `validateItemList` (`contextsummary/summary.go:69-77`),
detects a blank item by comparing `strings.TrimSpace(item) == ""`
(`:71`), but keys its duplicate-detection `seen` map on the raw,
untrimmed `item` (`:69`, `:74`). `"ship it"` and `"ship it "` both
pass as distinct, non-duplicate list items, even though the blank
check already proves this package treats the trimmed form as the
meaningful one for the same field. `skills.Skill.Validate`
(`skills/skill.go:56-74`) already folds a duplicate check onto the
trimmed form for the same kind of claim; this fix matches that
pattern.

### Fix scope

Inside:

- `validateItemList`, in `contextsummary/summary.go`, keys its `seen`
  map on `strings.TrimSpace(item)` instead of the raw `item`. The
  stored, returned, and rendered item stays exactly as the caller
  supplied it: `Summary.Decisions`, `OpenWork`, and `Risks` keep their
  original strings, including any surrounding whitespace on a
  non-duplicate entry. Only the map key used to detect a duplicate
  changes.
- `Summary.Validate`'s doc comment stays "no duplicate items"; the
  claim already matches the corrected behavior once the key trims.

Outside:

- `validateTextField`'s blank check on `Objective` and `State`. Those
  two fields carry no duplicate-detection map; this fix does not
  touch them.
- Case folding. `skills.Skill.Validate` compares triggers with
  `strings.EqualFold` after trim; this fix trims only, matching
  `Summary.Validate`'s own existing blank check, which also does not
  fold case. Adding fold here would be a scope increase this fix does
  not make.
- `Render`. It already renders the stored, untrimmed item; this fix
  does not change what a caller sees in the rendered text.

### Fix API

No exported symbol changes. `make api-update` must produce no diff
for `api/contextsummary.txt`. No `policy/layers.json` change: this
fix adds no import; `strings` is already imported by
`contextsummary/summary.go`.

### Fix tests

In `contextsummary/contextsummary_test/summary_test.go`:

- `TestValidateRejectsDuplicateAfterTrim` — a `Summary` with
  `Decisions: []string{"ship it", "ship it "}` fails `Validate` with
  an error matching the duplicate-item message. Fails against today's
  code, which treats the two strings as distinct and returns nil. One
  table-driven case parameterizes the same shape over `Decisions`,
  `OpenWork`, and `Risks`, since each list runs through the same
  `validateItemList` call independently.
- Positive control: a `Summary` with `Decisions: []string{"ship it",
  "ship it now"}` (two items that share a prefix but differ after
  trim) passes `Validate`. Proves the fix does not over-match on a
  shared prefix.
- Positive control: a `Summary` whose single `Decisions` entry carries
  leading or trailing whitespace, with no second entry, still passes
  `Validate` and the returned `Summary.Decisions[0]` still carries
  that whitespace unchanged. Proves the fix does not rewrite stored
  data.

### Fix verification

- `make verify` passes; `contextsummary` holds the 85 coverage floor.
- `go test -race ./contextsummary/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass. No `policy/layers.json` change.
- `docs/packages/contextsummary.md` line 44's `Summary.Validate()`
  entry needs no wording change: "no duplicate items" already matches
  the corrected code.

## Addendum: host-schema keys, evidence fields, preamble, and skip sentinel

Status: shipped.

### Addendum goal

Align `Summary` with the host durable schema in the sibling consumer
repo (`internal/contextmgr/contracts.go` lines 188-198). The SDK
document and the host document then share one JSON key set. Four
additions ship together: two fields, tagged keys, a preamble, and a
sentinel.

`Summary` stays a data-only document. The SDK does not port the host
keys `version` and `source_range`. The caller owns durability and
provenance, so those two fields stay with the caller's storage.

### Addendum scope

Inside:

- `Summary` grows `Evidence []string` and `ChangedSurfaces []string`.
  All seven fields gain json tags: `objective`, `state`, `decisions`,
  `evidence`, `changed_surfaces`, `open_work`, `risks`. The five
  lists carry `omitempty`; `objective` and `state` do not.
- `Validate` runs `validateItemList` over both new lists with the
  same bounds: `MaxItems`, `MaxFieldBytes`, no duplicates, no
  blanks. Check order follows field order: Objective, State,
  Decisions, Evidence, ChangedSurfaces, OpenWork, Risks.
- `Render` writes both new sections through `writeItems`, with the
  labels `Evidence:` and `ChangedSurfaces:` and `- ` bullets. Field
  order: Objective, State, Decisions, Evidence, ChangedSurfaces,
  OpenWork, Risks.
- `systemPrompt` states the tagged snake_case keys, pinned under
  Addendum API.
- `SummaryPreamble` and the `SummaryMessage` join, pinned under
  Addendum API. `SummaryPreamble` lives in `summary.go` beside
  `SummaryMessage`.
- `ErrSummarySkipped`, the skip sentinel, pinned under Addendum API.
  It lives in `summarizer.go` with the other sentinels.
- The reply-fixture updates listed under Addendum tests: the complete
  set, verified by grep for the capitalized key across every `*.go`
  file.
- `docs/packages/contextsummary.md`: the `Summary` bullet, the
  `SummaryPreamble` constant entry, the `SummaryMessage` bullet, and
  one `ErrSummarySkipped` failure-mode entry.
- `docs/architecture.md`: the `contextsummary` module-map bullet
  gains `SummaryPreamble` and `ErrSummarySkipped` in the same commit.

Outside:

- The host keys `version` and `source_range`. The document stays
  data-only and the caller owns durability.
- Any `agentloop.compactHistory` change that consumes
  `ErrSummarySkipped`. A follow-up addendum to
  `docs/plans/agentloop.md`, later in this same session, lands that
  consumer and removes the pending-symbols entry.
- Any strictness change to `decodeReply`. `DisallowUnknownFields`
  stays and the function stays byte-identical. Capitalized replies
  fail through the tags alone.
- `policy/layers.json`: no change. `contextsummary` still imports
  only `provider`.
- Timeouts, retry behavior, `TokenEstimate`, and excerpt caps: no
  change.

### Addendum API

The surface below is the lock target, landed through
`make api-update` in the same commit as the code.

```go
// Summary is one validated summary document. Data only: no tool,
// policy, or credential fields. The json tags pin the host durable
// schema keys; version and source_range stay with the caller.
type Summary struct {
    Objective       string   `json:"objective"`
    State           string   `json:"state"`
    Decisions       []string `json:"decisions,omitempty"`
    Evidence        []string `json:"evidence,omitempty"`
    ChangedSurfaces []string `json:"changed_surfaces,omitempty"`
    OpenWork        []string `json:"open_work,omitempty"`
    Risks           []string `json:"risks,omitempty"`
}

// SummaryPreamble is the framing line SummaryMessage places before
// Render output; Render itself carries no preamble.
const SummaryPreamble = "This message restates the conversation that compaction removed."

// ErrSummarySkipped is the sentinel a summarize adapter returns to
// decline summary injection; the concrete Summarizer never returns it.
var ErrSummarySkipped = errors.New("contextsummary: summary skipped")
```

`SummaryMessage` sets `Content` to exactly `SummaryPreamble + "\n" +
s.Render()`. No other join exists. An adapter that cannot summarize
returns `ErrSummarySkipped`; the caller then declines injection. The
concrete `*Summarizer` never returns this sentinel.

`systemPrompt` becomes:

```go
const systemPrompt = "Summarize the conversation excerpt for an agent. " +
	"Reply with one JSON object and nothing else. The object keys are " +
	"\"objective\" (string), \"state\" (string), \"decisions\" (array of " +
	"strings), \"evidence\" (array of strings), \"changed_surfaces\" " +
	"(array of strings), \"open_work\" (array of strings), and \"risks\" " +
	"(array of strings). No other keys. One markdown code fence around " +
	"the object is allowed. Objective and State are non-empty. Every " +
	"list item is non-blank and unique."
```

Why capitalized replies fail with no `decodeReply` change:
`encoding/json` folds case after the exact-tag miss, so `Objective`
still decodes into `objective`. `OpenWork` and `ChangedSurfaces`
cannot fold onto `open_work` and `changed_surfaces`; the underscore
breaks the fold. `DisallowUnknownFields` then rejects the key with
`ErrInvalidReply`. A reply keyed only `Objective`, `State`,
`Decisions`, and `Risks` still decodes through that fold. After the
edits above, one capitalized reply fixture remains in the tree: the
new rejection-test literal. It carries `OpenWork`, which is what
makes it fail.

### Addendum tests

All in `contextsummary/contextsummary_test/`, table-driven where the
case set grows.

- `TestSummaryJSONRoundTrip` — marshal one full `Summary`, decode the
  bytes back, every field equals. The same document with empty lists
  marshals to bytes without the five list keys; the `objective` and
  `state` keys always appear.
- The existing tables extend over both new lists, same shape as the
  three shipped lists. `TestSummaryValidateValidShapes` gains one
  valid case per new list. `TestSummaryValidateFieldBounds` gains one
  oversized item per new list. `TestSummaryValidateListRules` gains,
  per new list, one over-full list, one duplicate, and one blank
  item.
- `TestSummarizeRejectsCapitalizedKeyReply` — the old capitalized
  reply shape, kept as one local literal carrying `OpenWork`, fails
  `Summarize` with `ErrInvalidReply`.
- `TestSummaryMessagePreamble` — `SummaryMessage` content starts
  with `SummaryPreamble`, then one `\n`, then exactly the `Render`
  output. The `Render` output alone contains no preamble.
- The content equality in `TestSummaryMessage` becomes
  `SummaryPreamble + "\n" + s.Render()`.
- `TestRenderShowsEveryField` gains the `Evidence:` and
  `ChangedSurfaces:` labels with one bullet each, and the order chain
  extends to Decisions, Evidence, ChangedSurfaces, OpenWork.

Reply-fixture updates, the complete set. A grep for `"Objective"`
across every `*.go` file found the reply literals. A byte-boundary
pass over the budget tests found three more sites:

- `contextsummary/contextsummary_test/helper_test.go:65` —
  `validReply` becomes snake_case and gains `evidence` and
  `changed_surfaces`.
- `contextsummary/contextsummary_test/summarizer_test.go:101-119` —
  every reply literal in the invalid-reply table and the fenced
  cases becomes snake_case.
- `contextsummary/contextsummary_test/render_test.go` — the
  `TestSummaryMessage` equality above.
- `e2e/e2e_test/anthropic_compaction_test.go:54` — the `summaryJSON`
  reply.
- `agentloop/agentloop_test/compaction_test.go:72` —
  `summaryReplyJSON`.
- `agentloop/agentloop_test/compaction_budget_test.go:221` —
  `hugeReply`; snake_case keys, no new keys. The test expects
  failure, and the preamble only enlarges the reply; the boundary
  arithmetic below does not touch it.
- `agentloop/agentloop_test/compaction_budget_test.go:262-307` —
  `TestRunCompactedHistoryExactlyAtBudgetPasses` pins an exact-byte
  boundary. Today: empty system, the 50-byte rendered summary at
  line 280, one user byte, total 51. The change adds 91 bytes per
  rendered summary: 64 for `SummaryPreamble` plus its join newline,
  27 for the two new label lines. `Render` writes a label line for
  every section, empty list included, so the 27 bytes apply to every
  summary. Raise `MaxTokens` 51 to 142, and update the comment's
  arithmetic. Keep the sent-bytes equality assertion.
- `agentloop/agentloop_test/compaction_budget_test.go:382-414` —
  `TestCheckCompactedBudgetAtBudgetPasses` pins "1 + 81 + 1 = 83" in
  its comment. The same 91 bytes make it "1 + 172 + 1 = 174". Raise
  `MaxTokens` 83 to 174, and update the comment.
- `agentloop/agentloop_test/compaction_budget_test.go:416-444` —
  `TestCheckCompactedBudgetOverBudgetFails` shares that window. At
  `MaxTokens` 174, its "uu" user message totals 175, one byte over.
  Keep "uu", and update the comment. Without the window bump the
  test still fails, but the Budget()+1 boundary goes vacuous.
- `agentloop/agentloop_test/compaction_recovery_test.go:44` — the
  prefix check becomes `strings.HasPrefix(excerpts, "[user] " +
  contextsummary.SummaryPreamble)` plus one `Contains` on
  `Objective:`. The preamble now leads the summary message.
- `agentloop/agentloop_test/compaction_reentry_test.go:30` — the
  `Contains(m.Content, "Objective: Ship")` check still matches,
  because `Render` keeps its labels. Verify by running the test;
  change nothing blindly.
- `agentloop/agentloop_test/compaction_test.go`, two windows the
  byte-boundary pass missed. `checkCompactedBudget` re-estimates the
  rebuilt history through the calibrated estimator, so the 91-byte
  growth multiplies by the Observe-inflated factor.
  `TestRunAtExactTriggerCompacts` raises `MaxTokens` 100 to 200 and
  `TriggerPercent` 40 to 20. The trigger stays at exactly 40 tokens,
  the estimate the test pins. The rebuilt estimate lands at 174
  under factor 1. `TestRunUnderTriggerNoCompaction` raises
  `MaxTokens` 400 to 1600 and lowers `TriggerPercent` 40 to 10. The
  trigger stays at 160 tokens, today's exact value: the
  iteration-one estimate at 99 stays under it. The iteration-two raw
  bytes stay 99; the correction factor clamps at `MaxCorrectionFactor`,
  so the calibrated estimate caps at 198 and still trips. The
  rebuilt estimate, about 462, fits Budget 1600.
- `e2e/e2e_test/anthropic_compaction_test.go`,
  `TestAnthropicAgentLoopCompactionControl`. Raise `MaxTokens` 400 to
  800 and lower `TriggerPercent` 80 to 35. Budget 700 puts the
  trigger at 245: the iteration-one estimate at 200 stays under it,
  and the calibrated iteration-two estimate at about 308 trips it.
  The rebuilt estimate, about 500, fits Budget 700. `Iterations`
  stays 2 and the summary stays present.
- Each window bump preserves the pinned property: the exact trip,
  the under-trigger pass, or the two-iteration control flow. No
  assertion drops.

Pin for the `agentloop` and `e2e` reply fixtures: keep the five
existing keys, snake_cased. Do not add `evidence` or
`changed_surfaces` there. The key choice changes no byte counts:
`Render` writes both new label lines for every summary, empty lists
included. The pin keeps those fixtures minimal; `validReply` in
`contextsummary` alone exercises the new keys. A preamble rewording
re-derives the two boundary totals.

Fixture edits must not drop an assertion. The test-tampering gate
stays green with no trailer.

### Addendum verification

- `make verify` passes; `contextsummary` and the total hold the 85
  coverage floor.
- `make api-update` runs and `api/contextsummary.txt` gains
  `SummaryPreamble`, `ErrSummarySkipped`, and the two tagged
  `Summary` fields. The lock diff lands in the same commit as the
  code.
- `policy/pending_symbols.json` gains the entry below, in the same
  commit as the code. It cannot land earlier: the symbol-wiring gate
  reports a pending symbol that appears in no api lock as stale. The
  follow-up `agentloop` change removes the entry.

```json
"contextsummary.ErrSummarySkipped": {
  "reason": "Sentinel an adapter returns to decline summary injection; the concrete Summarizer never returns it. No caller yet: agentloop.compactHistory learns skip-not-fail handling in a follow-up addendum.",
  "target": "agentloop compaction addendum (docs/plans/agentloop.md), next change in this session",
  "permanent": false
}
```

- `SummaryPreamble` needs no entry. Its declaration plus the
  `SummaryMessage` use give two non-test occurrences in the module.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`,
  `scripts/check_prose.py`, `scripts/check_api.py`, and
  `scripts/check_symbol_wiring.py` pass.
- `policy/layers.json` carries no diff.
- No envelope conformance vector: `contextsummary` defines no wire
  format, so the schema change adds none.
