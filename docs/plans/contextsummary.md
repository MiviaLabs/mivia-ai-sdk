# Plan: contextsummary

Status: shipped. Ports the summarizer half of
`mivia-agent/internal/contextmgr`, simplified to this task's contract.
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
  four.

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

Status: planned, not yet built.

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
