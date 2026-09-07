package contextplan_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
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
	sum     contextplan.Summary
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
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name: "valid full summary",
			sum: contextplan.Summary{
				Objective: "Ship the release",
				State:     "Two tests fail",
				Decisions: []string{"Use SQLite"},
				OpenWork:  []string{"Fix tests"},
				Risks:     []string{"Deadline slips"},
			},
		},
		{
			name: "valid empty lists",
			sum:  contextplan.Summary{Objective: "o", State: "s"},
		},
		{
			name: "valid max field bytes",
			sum: contextplan.Summary{
				Objective: strings.Repeat("a", contextplan.MaxFieldBytes),
				State:     strings.Repeat("b", contextplan.MaxFieldBytes),
			},
		},
		{
			name: "valid decisions list at max items",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Decisions: repeatItems(contextplan.MaxItems),
			},
		},
		{
			name: "valid evidence list",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Evidence:  []string{"CI log"},
			},
		},
		{
			name: "valid changed surfaces list",
			sum: contextplan.Summary{
				Objective:       "o",
				State:           "s",
				ChangedSurfaces: []string{"api/envelope"},
			},
		},
	})
}

func TestSummaryValidateRequiredFields(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name:    "invalid empty objective",
			sum:     contextplan.Summary{State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid whitespace objective",
			sum:     contextplan.Summary{Objective: "  ", State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid empty state",
			sum:     contextplan.Summary{Objective: "o"},
			wantErr: true,
		},
		{
			name:    "invalid control character in objective",
			sum:     contextplan.Summary{Objective: "bad\x01", State: "s"},
			wantErr: true,
		},
		{
			name: "invalid utf8 in state",
			sum: contextplan.Summary{
				Objective: "o",
				State:     string([]byte{0xff, 0xfe}),
			},
			wantErr: true,
		},
	})
}

func TestSummaryValidateFieldBounds(t *testing.T) {
	overField := strings.Repeat("a", contextplan.MaxFieldBytes+1)
	runValidateCases(t, []struct {
		name    string
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name:    "invalid oversized objective",
			sum:     contextplan.Summary{Objective: overField, State: "s"},
			wantErr: true,
		},
		{
			name:    "invalid oversized state",
			sum:     contextplan.Summary{Objective: "o", State: overField},
			wantErr: true,
		},
		{
			name: "invalid oversized decisions item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized open work item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				OpenWork:  []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized risks item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized evidence item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Evidence:  []string{overField},
			},
			wantErr: true,
		},
		{
			name: "invalid oversized changed surfaces item",
			sum: contextplan.Summary{
				Objective:       "o",
				State:           "s",
				ChangedSurfaces: []string{overField},
			},
			wantErr: true,
		},
	})
}

func TestSummaryValidateListRules(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name: "invalid over full decisions list",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Decisions: repeatItems(contextplan.MaxItems + 1),
			},
			wantErr: true,
		},
		{
			name: "invalid duplicate decisions",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{"same", "same"},
			},
			wantErr: true,
		},
		{
			name: "invalid empty decisions item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Decisions: []string{""},
			},
			wantErr: true,
		},
		{
			name: "invalid blank open work item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				OpenWork:  []string{" "},
			},
			wantErr: true,
		},
		{
			name: "invalid blank risks item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{"\t"},
			},
			wantErr: true,
		},
		{
			name: "invalid control character in a risk item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Risks:     []string{"bad\x02"},
			},
			wantErr: true,
		},
	})
}

// TestSummaryValidateEvidenceListRules runs the shared list bounds
// over Evidence: one over-full list, one duplicate, one blank item.
func TestSummaryValidateEvidenceListRules(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name: "invalid over full evidence list",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Evidence:  repeatItems(contextplan.MaxItems + 1),
			},
			wantErr: true,
		},
		{
			name: "invalid duplicate evidence",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Evidence:  []string{"same", "same"},
			},
			wantErr: true,
		},
		{
			name: "invalid blank evidence item",
			sum: contextplan.Summary{
				Objective: "o",
				State:     "s",
				Evidence:  []string{" "},
			},
			wantErr: true,
		},
	})
}

// TestSummaryValidateChangedSurfacesListRules runs the shared list
// bounds over ChangedSurfaces: one over-full list, one duplicate, one
// blank item.
func TestSummaryValidateChangedSurfacesListRules(t *testing.T) {
	runValidateCases(t, []struct {
		name    string
		sum     contextplan.Summary
		wantErr bool
	}{
		{
			name: "invalid over full changed surfaces list",
			sum: contextplan.Summary{
				Objective:       "o",
				State:           "s",
				ChangedSurfaces: repeatItems(contextplan.MaxItems + 1),
			},
			wantErr: true,
		},
		{
			name: "invalid duplicate changed surfaces",
			sum: contextplan.Summary{
				Objective:       "o",
				State:           "s",
				ChangedSurfaces: []string{"same", "same"},
			},
			wantErr: true,
		},
		{
			name: "invalid blank changed surfaces item",
			sum: contextplan.Summary{
				Objective:       "o",
				State:           "s",
				ChangedSurfaces: []string{""},
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
		sum  contextplan.Summary
	}{
		{
			name: "decisions",
			sum: contextplan.Summary{
				Objective: "o", State: "s",
				Decisions: []string{"ship it", "ship it "},
			},
		},
		{
			name: "open work",
			sum: contextplan.Summary{
				Objective: "o", State: "s",
				OpenWork: []string{"ship it", "ship it "},
			},
		},
		{
			name: "risks",
			sum: contextplan.Summary{
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
	sum := contextplan.Summary{
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
	sum := contextplan.Summary{
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

// TestSummaryJSONRoundTrip pins the tagged wire shape: one full
// Summary marshals and decodes back field-equal. Empty lists marshal
// without their keys, so the five list tags carry omitempty and the
// objective and state tags never do.
func TestSummaryJSONRoundTrip(t *testing.T) {
	full := contextplan.Summary{
		Objective:       "Ship the release",
		State:           "Two tests fail",
		Decisions:       []string{"Use SQLite"},
		Evidence:        []string{"CI log"},
		ChangedSurfaces: []string{"api/envelope"},
		OpenWork:        []string{"Fix tests"},
		Risks:           []string{"Deadline slips"},
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var back contextplan.Summary
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal() = %v, want nil", err)
	}
	if !reflect.DeepEqual(back, full) {
		t.Fatalf("round trip = %+v, want %+v", back, full)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("Unmarshal() into keys = %v, want nil", err)
	}
	for _, key := range []string{
		"objective", "state", "decisions", "evidence",
		"changed_surfaces", "open_work", "risks",
	} {
		if _, ok := keys[key]; !ok {
			t.Fatalf("full marshal lacks key %q: %s", key, raw)
		}
	}
	if len(keys) != 7 {
		t.Fatalf("full marshal carries %d keys, want 7: %s", len(keys), raw)
	}

	bare := contextplan.Summary{Objective: "o", State: "s"}
	rawBare, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	var bareKeys map[string]json.RawMessage
	if err := json.Unmarshal(rawBare, &bareKeys); err != nil {
		t.Fatalf("Unmarshal() into keys = %v, want nil", err)
	}
	for _, key := range []string{
		"decisions", "evidence", "changed_surfaces", "open_work", "risks",
	} {
		if _, ok := bareKeys[key]; ok {
			t.Fatalf("empty-list marshal carries key %q: %s", key, rawBare)
		}
	}
	for _, key := range []string{"objective", "state"} {
		if _, ok := bareKeys[key]; !ok {
			t.Fatalf("marshal lacks always-present key %q: %s", key, rawBare)
		}
	}
}
