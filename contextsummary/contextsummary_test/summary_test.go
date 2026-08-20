package contextsummary_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
)

// repeatItems builds n distinct, valid list items.
func repeatItems(n int) []string {
	items := make([]string, n)
	for i := range items {
		items[i] = strings.Repeat("d", i+1)
	}
	return items
}

// runValidateCases runs one table of Validate cases.
func runValidateCases(t *testing.T, cases []struct {
	name    string
	sum     contextsummary.Summary
	wantErr bool
}) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.sum.Validate()
			if c.wantErr && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestSummaryValidateValidShapes(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextsummary.Summary
		wantErr bool
	}{
		{
			name: "valid full summary",
			sum: contextsummary.Summary{
				Objective: "Ship the release",
				State:     "Two tests fail",
				Decisions: []string{"Use SQLite"},
				OpenWork:  []string{"Fix tests"},
				Risks:     []string{"Deadline slips"},
			},
		},
		{
			name: "valid empty lists",
			sum:  contextsummary.Summary{Objective: "o", State: "s"},
		},
		{
			name: "valid max field bytes",
			sum: contextsummary.Summary{
				Objective: strings.Repeat("a", contextsummary.MaxFieldBytes),
				State:     strings.Repeat("b", contextsummary.MaxFieldBytes),
			},
		},
		{
			name: "valid decisions list at max items",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Decisions: repeatItems(contextsummary.MaxItems),
			},
		},
	})
}

func TestSummaryValidateRequiredFields(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextsummary.Summary
		wantErr bool
	}{
		{
			name:    "invalid empty objective",
			sum:     contextsummary.Summary{State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid whitespace objective",
			sum:     contextsummary.Summary{Objective: "  ", State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid empty state",
			sum:     contextsummary.Summary{Objective: "o"},
			wantErr: true,
		},
		{
			name:    "invalid control character in objective",
			sum:     contextsummary.Summary{Objective: "bad\x01", State: "s"},
			wantErr: true,
		},
		{
			name: "invalid utf8 in state",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     string([]byte{0xff, 0xfe}),
			},
			wantErr: true,
		},
	})
}

func TestSummaryValidateFieldBounds(t *testing.T) {
	overField := strings.Repeat("a", contextsummary.MaxFieldBytes+1)
	runValidateCases(t, []struct {
		name    string
		sum     contextsummary.Summary
		wantErr bool
	}{
		{
			name:    "invalid oversized objective",
			sum:     contextsummary.Summary{Objective: overField, State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid oversized state",
			sum:     contextsummary.Summary{Objective: "o", State: overField},
			wantErr: true,
		},
		{
			name: "invalid oversized decisions item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized open work item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				OpenWork:  []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized risks item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{overField},
			},
			wantErr: true,
		},
	})
}

func TestSummaryValidateListRules(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextsummary.Summary
		wantErr bool
	}{
		{
			name: "invalid over full decisions list",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Decisions: repeatItems(contextsummary.MaxItems + 1),
			},
			wantErr: true,
		},
		{
			name: "invalid duplicate decisions",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{"same", "same"},
			},
			wantErr: true,
		},
		{
			name: "invalid empty decisions item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{""},
			},
			wantErr: true,
		},
		{
			name: "invalid blank open work item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				OpenWork:  []string{" "},
			},
			wantErr: true,
		},
		{
			name: "invalid blank risks item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{"\t"},
			},
			wantErr: true,
		},
		{
			name: "invalid control character in a risk item",
			sum: contextsummary.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{"bad\x02"},
			},
			wantErr: true,
		},
	})
}

// TestValidateRejectsDuplicateAfterTrim fails against today's code,
// which keys duplicate detection on the raw item, so "ship it" and
// "ship it " pass as distinct. One case per list: each runs through
// validateItemList independently.
func TestValidateRejectsDuplicateAfterTrim(t *testing.T) {
	cases := []struct {
		name string
		sum  contextsummary.Summary
	}{
		{
			name: "decisions",
			sum: contextsummary.Summary{
				Objective: "o", State: "s",
				Decisions: []string{"ship it", "ship it "},
			},
		},
		{
			name: "open work",
			sum: contextsummary.Summary{
				Objective: "o", State: "s",
				OpenWork: []string{"ship it", "ship it "},
			},
		},
		{
			name: "risks",
			sum: contextsummary.Summary{
				Objective: "o", State: "s",
				Risks: []string{"ship it", "ship it "},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.sum.Validate(); err == nil {
				t.Fatal("Validate() = nil, want a duplicate-item error")
			}
		})
	}
}

// TestValidateAcceptsSharedPrefixItems is a positive control: two
// items that share a prefix but differ after trim are not duplicates.
func TestValidateAcceptsSharedPrefixItems(t *testing.T) {
	sum := contextsummary.Summary{
		Objective: "o", State: "s",
		Decisions: []string{"ship it", "ship it now"},
	}
	if err := sum.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestValidateKeepsStoredWhitespace is a positive control: a single
// list entry with surrounding whitespace and no duplicate still
// passes, and the returned Decisions[0] keeps that whitespace
// unchanged, proving the fix does not rewrite stored data.
func TestValidateKeepsStoredWhitespace(t *testing.T) {
	sum := contextsummary.Summary{
		Objective: "o", State: "s",
		Decisions: []string{"ship it "},
	}
	if err := sum.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if sum.Decisions[0] != "ship it " {
		t.Fatalf("Decisions[0] = %q, want %q", sum.Decisions[0], "ship it ")
	}
}
