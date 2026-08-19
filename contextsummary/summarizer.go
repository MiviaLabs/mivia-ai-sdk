package contextsummary

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
	// ErrNilCompleter is NewSummarizer's error for a nil Completer.
	ErrNilCompleter = errors.New("contextsummary: completer is required")
	// ErrNoMessages is Summarize's error for an empty message list.
	ErrNoMessages = errors.New("contextsummary: no messages to summarize")
	// ErrInvalidReply is Summarize's error when the reply fails
	// strict parsing or Summary.Validate.
	ErrInvalidReply = errors.New("contextsummary: reply failed strict parsing or validation")
	// ErrCallFailed is Summarize's error when the Completer call
	// itself fails.
	ErrCallFailed = errors.New("contextsummary: summary call failed")
)

// codeFence delimits the one markdown code fence a reply may carry.
const codeFence = "```"

// systemPrompt states the summarize task and the exact JSON reply
// schema, with the Summary field names encoding/json decodes.
const systemPrompt = "Summarize the conversation excerpt for an agent. " +
	"Reply with one JSON object and nothing else. The object keys are " +
	"\"Objective\" (string), \"State\" (string), \"Decisions\" (array of " +
	"strings), \"OpenWork\" (array of strings), and \"Risks\" (array of " +
	"strings). No other keys. One markdown code fence around the object " +
	"is allowed. Objective and State are non-empty. Every list item is " +
	"non-blank and unique."

// Summarizer adapts one provider.Completer to summary generation.
type Summarizer struct {
	completer provider.Completer
}

// NewSummarizer binds one Completer. A nil Completer wraps
// ErrNilCompleter.
func NewSummarizer(c provider.Completer) (*Summarizer, error) {
	if c == nil {
		return nil, fmt.Errorf("contextsummary: %w", ErrNilCompleter)
	}
	return &Summarizer{completer: c}, nil
}

// Summarize makes one bounded Completer call over msgs and returns the
// validated Summary. Never retries. Any failure is caller-visible.
func (s *Summarizer) Summarize(ctx context.Context, msgs []provider.Message) (Summary, error) {
	if len(msgs) == 0 {
		return Summary{}, ErrNoMessages
	}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: systemPrompt},
			{Role: provider.RoleUser, Content: buildExcerpts(msgs)},
		},
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
