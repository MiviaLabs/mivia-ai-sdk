package contextsummary

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// MaxFieldBytes bounds every individual Summary text field and every
// list item.
const MaxFieldBytes = 2 * 1024

// MaxItems bounds every Summary list.
const MaxItems = 32

// MaxExcerptTotalBytes bounds the whole source-excerpt section of one
// summarize prompt.
const MaxExcerptTotalBytes = 16 * 1024

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
func (s Summary) Validate() error {
	if err := validateTextField("Objective", s.Objective); err != nil {
		return err
	}
	if err := validateTextField("State", s.State); err != nil {
		return err
	}
	if err := validateItemList("Decisions", s.Decisions); err != nil {
		return err
	}
	if err := validateItemList("OpenWork", s.OpenWork); err != nil {
		return err
	}
	return validateItemList("Risks", s.Risks)
}

// validateTextField bounds one required text field.
func validateTextField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("contextsummary: %s is required", field)
	}
	return validateTextForm(field, value)
}

// validateItemList bounds one item list.
func validateItemList(field string, items []string) error {
	if len(items) > MaxItems {
		return fmt.Errorf("contextsummary: %s has %d items, max %d", field, len(items), MaxItems)
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("contextsummary: %s has a blank item", field)
		}
		if _, dup := seen[item]; dup {
			return fmt.Errorf("contextsummary: %s has a duplicate item", field)
		}
		seen[item] = struct{}{}
		if err := validateTextForm(field, item); err != nil {
			return err
		}
	}
	return nil
}

// validateTextForm rejects text over MaxFieldBytes, invalid UTF-8, or
// a control character.
func validateTextForm(field, value string) error {
	if len(value) > MaxFieldBytes || !utf8.ValidString(value) {
		return fmt.Errorf("contextsummary: %s is invalid or too long", field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("contextsummary: %s contains a control character", field)
		}
	}
	return nil
}

// Render returns the deterministic text form of s: one labeled line
// or bullet per field, in field order.
func (s Summary) Render() string {
	var b strings.Builder
	b.WriteString("Objective: " + s.Objective + "\n")
	b.WriteString("State: " + s.State + "\n")
	writeItems(&b, "Decisions", s.Decisions)
	writeItems(&b, "OpenWork", s.OpenWork)
	writeItems(&b, "Risks", s.Risks)
	return b.String()
}

// writeItems writes one labeled line and one bullet per item.
func writeItems(b *strings.Builder, field string, items []string) {
	b.WriteString(field + ":\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
}

// SummaryMessage renders s as one RoleUser message named
// SummaryMessageName, whose Content is s.Render().
func SummaryMessage(s Summary) provider.Message {
	return provider.Message{
		Role:    provider.RoleUser,
		Name:    SummaryMessageName,
		Content: s.Render(),
	}
}

// TokenEstimate prices n bytes at n/4 tokens, minimum one for
// non-zero input, zero for zero input.
func TokenEstimate(n int) int {
	if n <= 0 {
		return 0
	}
	tokens := n / 4
	if tokens == 0 {
		return 1
	}
	return tokens
}
