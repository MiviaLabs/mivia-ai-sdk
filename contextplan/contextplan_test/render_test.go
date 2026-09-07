package contextplan_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestRenderDeterministic(t *testing.T) {
	s := contextplan.Summary{
		Objective: "Ship the release",
		State:     "Two tests fail",
		Decisions: []string{"Use SQLite"},
		OpenWork:  []string{"Fix tests"},
		Risks:     []string{"Deadline slips"},
	}
	first := s.Render()
	second := s.Render()
	if first != second {
		t.Fatalf("Render() not deterministic:\n%q\n%q", first, second)
	}
}

func TestRenderShowsEveryField(t *testing.T) {
	s := contextplan.Summary{
		Objective:       "obj-text",
		State:           "state-text",
		Decisions:       []string{"decision-text"},
		Evidence:        []string{"evidence-text"},
		ChangedSurfaces: []string{"changedsurfaces-text"},
		OpenWork:        []string{"openwork-text"},
		Risks:           []string{"risk-text"},
	}
	got := s.Render()
	for _, want := range []string{
		"Objective:", "obj-text",
		"State:", "state-text",
		"Decisions:", "- decision-text",
		"Evidence:", "- evidence-text",
		"ChangedSurfaces:", "- changedsurfaces-text",
		"OpenWork:", "- openwork-text",
		"Risks:", "- risk-text",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render() missing %q:\n%s", want, got)
		}
	}
	chain := []string{
		"Objective:", "State:", "Decisions:", "Evidence:",
		"ChangedSurfaces:", "OpenWork:", "Risks:",
	}
	for i := 0; i+1 < len(chain); i++ {
		if strings.Index(got, chain[i]) > strings.Index(got, chain[i+1]) {
			t.Fatalf("Render() %s does not precede %s:\n%s", chain[i], chain[i+1], got)
		}
	}
}

func TestRenderEqualSummariesRenderEqualText(t *testing.T) {
	a := contextplan.Summary{Objective: "o", State: "s", Risks: []string{"r1", "r2"}}
	b := contextplan.Summary{Objective: "o", State: "s", Risks: []string{"r1", "r2"}}
	if a.Render() != b.Render() {
		t.Fatalf("equal summaries rendered differently:\n%q\n%q", a.Render(), b.Render())
	}
}

func TestSummaryMessage(t *testing.T) {
	s := contextplan.Summary{Objective: "o", State: "s"}
	msg := contextplan.SummaryMessage(s)
	if msg.Role != provider.RoleUser {
		t.Fatalf("SummaryMessage role = %q, want %q", msg.Role, provider.RoleUser)
	}
	if msg.Name != contextplan.SummaryMessageName {
		t.Fatalf("SummaryMessage name = %q, want %q", msg.Name, contextplan.SummaryMessageName)
	}
	want := contextplan.SummaryPreamble + "\n" + s.Render()
	if msg.Content != want {
		t.Fatalf("SummaryMessage content = %q, want %q", msg.Content, want)
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("SummaryMessage Validate() = %v, want nil", err)
	}
}

// TestSummaryMessagePreamble pins the join: the content is the
// preamble, one newline, then exactly the Render output. Render alone
// carries no preamble.
func TestSummaryMessagePreamble(t *testing.T) {
	s := contextplan.Summary{Objective: "o", State: "s"}
	msg := contextplan.SummaryMessage(s)
	if !strings.HasPrefix(msg.Content, contextplan.SummaryPreamble) {
		t.Fatalf("SummaryMessage content lacks the preamble prefix: %q", msg.Content)
	}
	rest, ok := strings.CutPrefix(msg.Content, contextplan.SummaryPreamble)
	if !ok || rest == "" || rest[0] != '\n' {
		t.Fatalf("SummaryMessage content lacks one newline after the preamble: %q", msg.Content)
	}
	if rest[1:] != s.Render() {
		t.Fatalf("SummaryMessage content after the preamble = %q, want Render() = %q", rest[1:], s.Render())
	}
	if strings.Contains(s.Render(), contextplan.SummaryPreamble) {
		t.Fatalf("Render() carries the preamble; only SummaryMessage joins it:\n%s", s.Render())
	}
}
