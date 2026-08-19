package contextsummary_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextsummary"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestRenderDeterministic(t *testing.T) {
	s := contextsummary.Summary{
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
	s := contextsummary.Summary{
		Objective: "obj-text",
		State:     "state-text",
		Decisions: []string{"decision-text"},
		OpenWork:  []string{"openwork-text"},
		Risks:     []string{"risk-text"},
	}
	got := s.Render()
	for _, want := range []string{
		"Objective:", "obj-text",
		"State:", "state-text",
		"Decisions:", "- decision-text",
		"OpenWork:", "- openwork-text",
		"Risks:", "- risk-text",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Render() missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "Objective:") > strings.Index(got, "State:") {
		t.Fatal("Render() objective does not precede state")
	}
	if strings.Index(got, "State:") > strings.Index(got, "Decisions:") {
		t.Fatal("Render() state does not precede decisions")
	}
}

func TestRenderEqualSummariesRenderEqualText(t *testing.T) {
	a := contextsummary.Summary{Objective: "o", State: "s", Risks: []string{"r1", "r2"}}
	b := contextsummary.Summary{Objective: "o", State: "s", Risks: []string{"r1", "r2"}}
	if a.Render() != b.Render() {
		t.Fatalf("equal summaries rendered differently:\n%q\n%q", a.Render(), b.Render())
	}
}

func TestSummaryMessage(t *testing.T) {
	s := contextsummary.Summary{Objective: "o", State: "s"}
	msg := contextsummary.SummaryMessage(s)
	if msg.Role != provider.RoleUser {
		t.Fatalf("SummaryMessage role = %q, want %q", msg.Role, provider.RoleUser)
	}
	if msg.Name != contextsummary.SummaryMessageName {
		t.Fatalf("SummaryMessage name = %q, want %q", msg.Name, contextsummary.SummaryMessageName)
	}
	if msg.Content != s.Render() {
		t.Fatalf("SummaryMessage content = %q, want Render() = %q", msg.Content, s.Render())
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("SummaryMessage Validate() = %v, want nil", err)
	}
}
