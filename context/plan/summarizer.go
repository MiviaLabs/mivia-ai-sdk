package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// SummaryTimeout bounds one summarize call.
const SummaryTimeout = 20 * time.Second

// Sentinel errors; test with errors.Is.
var (
	// ErrNoMessagesToSummarize is Summarize's error for an empty message list.
	ErrNoMessagesToSummarize = errors.New("plan: no messages to summarize")
	// ErrInvalidReply is Summarize's error when the reply fails
	// strict parsing or Summary.Validate.
	ErrInvalidReply = errors.New("plan: reply failed strict parsing or validation")
	// ErrCallFailed is Summarize's error when the Completer call
	// itself fails.
	ErrCallFailed = errors.New("plan: summary call failed")
	// ErrSummarySkipped is the sentinel a summarize adapter returns
	// to decline summary injection; the concrete Summarizer never
	// returns it. An adapter may wrap it with a reason, for example
	// fmt.Errorf("%w: %s", ErrSummarySkipped, reason); agentloop's
	// summarizeDropped matches the skip through errors.Is, so a
	// wrapped sentinel still takes the skip path and the wrapping
	// error's own message carries the reason to the caller.
	ErrSummarySkipped = errors.New("plan: summary skipped")
)

// codeFence delimits the one markdown code fence a reply may carry.
const codeFence = "```"

// systemPrompt states the summarize task and the exact JSON reply
// schema, with the tagged Summary keys encoding/json decodes. The
// snake_case keys make capitalized replies fail: OpenWork and
// ChangedSurfaces cannot case-fold onto open_work and changed_surfaces,
// so DisallowUnknownFields rejects them.
const systemPrompt = "Summarize the conversation excerpt for an agent. " +
	"Reply with one JSON object and nothing else. The object keys are " +
	"\"objective\" (string), \"state\" (string), \"decisions\" (array of " +
	"strings), \"evidence\" (array of strings), \"changed_surfaces\" " +
	"(array of strings), \"open_work\" (array of strings), and \"risks\" " +
	"(array of strings). No other keys. One markdown code fence around " +
	"the object is allowed. Objective and State are non-empty. Every " +
	"list item is non-blank and unique."

// Summarizer adapts one provider.Completer to summary generation.
// MaxTokens caps the summarize call's provider.Request.MaxTokens. Nil
// means the Completer's own default, the same behavior as before this
// field existed. Set it before the first Summarize call. Summarize
// reads MaxTokens exactly once, on the first call, and freezes that
// snapshot for every later call on this Summarizer; a write to
// MaxTokens after the first Summarize call has no effect. Summarize
// makes no promise about concurrent calls on the same Summarizer; use
// one Summarizer from one goroutine at a time.
type Summarizer struct {
	MaxTokens *int

	completer         provider.Completer
	maxTokensFrozen   bool
	maxTokensSnapshot *int
}

// NewSummarizer binds one Completer. A nil Completer wraps
// ErrInvalidOptions.
func NewSummarizer(c provider.Completer) (*Summarizer, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOptions, "Completer: is required")
	}
	return &Summarizer{completer: c}, nil
}

// Summarize makes one bounded Completer call over msgs and returns the
// validated Summary. Never retries. Any failure is caller-visible.
// Summarize reads s.MaxTokens exactly once, on the first call, and
// freezes it for every later call; see the Summarizer doc comment.
func (s *Summarizer) Summarize(ctx context.Context, msgs []provider.Message) (Summary, error) {
	if len(msgs) == 0 {
		return Summary{}, ErrNoMessagesToSummarize
	}
	if !s.maxTokensFrozen {
		if s.MaxTokens != nil {
			n := *s.MaxTokens
			s.maxTokensSnapshot = &n
		}
		s.maxTokensFrozen = true
	}
	if s.maxTokensSnapshot != nil && *s.maxTokensSnapshot <= 0 {
		// Window shares the sentinel: a zero token cap can never
		// produce a usable reply, here or in a planned request.
		return Summary{}, ErrMaxTokensNotPositive
	}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: systemPrompt},
			{Role: provider.RoleUser, Content: buildExcerpts(msgs)},
		},
	}
	if s.maxTokensSnapshot != nil {
		n := *s.maxTokensSnapshot
		req.MaxTokens = &n
	}
	tctx, cancel := context.WithTimeout(ctx, SummaryTimeout)
	defer cancel()
	resp, err := s.completer.Chat(tctx, req)
	if err != nil {
		return Summary{}, fmt.Errorf("%w: %w", ErrCallFailed, err)
	}
	return decodeReply(resp.Message.Content)
}

// buildExcerpts renders the excerpt section: newest message first,
// each excerpt capped at MaxFieldBytes, the whole section capped at
// MaxExcerptTotalBytes. The walk stops at the first excerpt that does
// not fit the remaining total. Summarize never mutates msgs.
func buildExcerpts(msgs []provider.Message) string {
	var b strings.Builder
	remaining := MaxExcerptTotalBytes
	for i := len(msgs) - 1; i >= 0; i-- {
		excerpt := capExcerpt(fmt.Sprintf("[%s] %s", msgs[i].Role, msgs[i].Content))
		if len(excerpt)+1 > remaining {
			break
		}
		b.WriteString(excerpt)
		b.WriteString("\n")
		remaining -= len(excerpt) + 1
	}
	return b.String()
}

// capExcerpt cuts one excerpt to MaxFieldBytes, dropping every invalid
// UTF-8 byte in the cut prefix, the same fix contextplan's StubContent
// applies.
func capExcerpt(s string) string {
	if len(s) <= MaxFieldBytes {
		return s
	}
	return string(bytes.ToValidUTF8([]byte(s[:MaxFieldBytes]), nil))
}

// decodeReply parses one reply into a validated Summary. It accepts at
// most one markdown code fence, rejects unknown fields, empty replies,
// and trailing bytes, through encoding/json with
// DisallowUnknownFields.
func decodeReply(raw string) (Summary, error) {
	body, err := stripFence(strings.TrimSpace(raw))
	if err != nil {
		return Summary{}, err
	}
	if body == "" {
		return Summary{}, errInvalid("empty reply")
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var out Summary
	if err := dec.Decode(&out); err != nil {
		return Summary{}, errInvalid("reply is not the schema object")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Summary{}, errInvalid("trailing bytes after the object")
	}
	if err := out.Validate(); err != nil {
		return Summary{}, errInvalid("reply failed validation")
	}
	return out, nil
}

// stripFence removes one enclosing markdown code fence. A reply with
// no fence passes through; a reply with anything but exactly one
// enclosing fence fails.
func stripFence(s string) (string, error) {
	n := strings.Count(s, codeFence)
	if n == 0 {
		return s, nil
	}
	if n != 2 || !strings.HasPrefix(s, codeFence) || !strings.HasSuffix(s, codeFence) {
		return "", errInvalid("reply carries more than one code fence")
	}
	body := s[len(codeFence):]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		return "", errInvalid("code fence has no body")
	}
	body = body[:len(body)-len(codeFence)]
	return strings.TrimSpace(body), nil
}

// errInvalid wraps ErrInvalidReply with the failing reason.
func errInvalid(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidReply, reason)
}
