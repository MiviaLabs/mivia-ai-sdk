package longtermmemory

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Tier and bound constants.
const (
	// CoreTierCap bounds core-tier entries per scope.
	CoreTierCap = 24
	// DefaultMaxEntries caps rows per scope when New receives zero.
	DefaultMaxEntries = 500
	// DefaultMaxSearchResults caps search results when Query carries
	// zero.
	DefaultMaxSearchResults = 8
	// DefaultFrameBytes caps CoreFrame output when maxBytes is zero.
	DefaultFrameBytes = 4 * 1024
	// ConsolidateLoadFactor is the fill ratio that triggers
	// consolidation.
	ConsolidateLoadFactor = 0.8
)

// Entry field bounds.
const (
	maxTitleRunes   = 120
	maxSummaryRunes = 400
	maxDetailRunes  = 2000
	maxTags         = 8
	maxTagRunes     = 32
	dateLayout      = "2006-01-02"
)

// Verdict is the agent's assessment of one recorded experience.
type Verdict string

// The closed Verdict set.
const (
	VerdictGood    Verdict = "good"
	VerdictBad     Verdict = "bad"
	VerdictMixed   Verdict = "mixed"
	VerdictNeutral Verdict = "neutral"
)

// Entry is one memory. Created is YYYY-MM-DD; empty means today at
// save time.
type Entry struct {
	Title   string
	Scope   string
	Verdict Verdict
	Tags    []string
	Created string
	Summary string
	Detail  string
}

// Validate enforces: non-empty Title at most 120 runes and no line
// breaks; a non-blank Scope and a Verdict from the closed set;
// Summary required at most 400 runes; Detail at most 2000 runes; at
// most 8 tags, each at most 32 runes with no comma or line break;
// Created empty or a valid date; no control characters beyond LF and
// TAB in Summary and Detail.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("longtermmemory: title is required")
	}
	if utf8.RuneCountInString(e.Title) > maxTitleRunes || hasLineBreak(e.Title) {
		return fmt.Errorf("longtermmemory: title is invalid or too long")
	}
	if strings.TrimSpace(e.Scope) == "" {
		return fmt.Errorf("longtermmemory: scope is required")
	}
	if !validVerdict(e.Verdict) {
		return fmt.Errorf("longtermmemory: verdict %q is unknown", e.Verdict)
	}
	if strings.TrimSpace(e.Summary) == "" {
		return fmt.Errorf("longtermmemory: summary is required")
	}
	if utf8.RuneCountInString(e.Summary) > maxSummaryRunes {
		return fmt.Errorf("longtermmemory: summary is too long")
	}
	if utf8.RuneCountInString(e.Detail) > maxDetailRunes {
		return fmt.Errorf("longtermmemory: detail is too long")
	}
	if err := validateTags(e.Tags); err != nil {
		return err
	}
	if e.Created != "" {
		if _, err := time.Parse(dateLayout, e.Created); err != nil {
			return fmt.Errorf("longtermmemory: created is not a valid date")
		}
	}
	if hasForbiddenControl(e.Summary) || hasForbiddenControl(e.Detail) {
		return fmt.Errorf("longtermmemory: summary and detail allow only LF and TAB control characters")
	}
	return nil
}

// validateTags bounds the tag list and each tag's form.
func validateTags(tags []string) error {
	if len(tags) > maxTags {
		return fmt.Errorf("longtermmemory: %d tags, max %d", len(tags), maxTags)
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > maxTagRunes {
			return fmt.Errorf("longtermmemory: tag %q is too long", tag)
		}
		if strings.ContainsAny(tag, ",\n\r") {
			return fmt.Errorf("longtermmemory: tag %q carries a comma or line break", tag)
		}
	}
	return nil
}

// validVerdict reports whether v is one of the four constants.
func validVerdict(v Verdict) bool {
	switch v {
	case VerdictGood, VerdictBad, VerdictMixed, VerdictNeutral:
		return true
	}
	return false
}

// hasLineBreak reports whether s carries LF or CR.
func hasLineBreak(s string) bool {
	return strings.ContainsAny(s, "\n\r")
}

// hasForbiddenControl reports whether s carries a control character
// other than LF and TAB.
func hasForbiddenControl(s string) bool {
	for _, r := range s {
		if (r < 0x20 || r == 0x7f) && r != '\n' && r != '\t' {
			return true
		}
	}
	return false
}
