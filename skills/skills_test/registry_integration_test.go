package skills_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

// TestRegistryMatchAcrossDistinctSkills registers three skills for
// three distinct concerns, then proves Match resolves each query to
// the one skill it names, and that RequiredTools passes through
// unread and unenforced: a tool name absent from any real
// tools.Registry still registers and still matches.
func TestRegistryMatchAcrossDistinctSkills(t *testing.T) {
	r := skills.New()

	codeReview := skills.Skill{
		Name:          "code-review",
		Instructions:  "review the diff for correctness and style",
		Triggers:      []string{"code review", "review pr"},
		RequiredTools: []string{"git-diff"},
	}
	deploy := skills.Skill{
		Name:          "deploy",
		Instructions:  "ship the release through the deploy pipeline",
		Triggers:      []string{"deploy", "release"},
		RequiredTools: []string{"deploy-cli", "no-such-tool"},
	}
	incident := skills.Skill{
		Name:          "incident-response",
		Instructions:  "triage the outage and page the on-call",
		Triggers:      []string{"incident", "outage"},
		RequiredTools: []string{"pager"},
	}

	for _, s := range []skills.Skill{codeReview, deploy, incident} {
		if err := r.Add(s); err != nil {
			t.Fatalf("Add(%s) error = %v, want nil", s.Name, err)
		}
	}

	got := r.Match("deploy")
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Fatalf("Match(deploy) = %v, want [deploy]", got)
	}
	if len(got[0].RequiredTools) != 2 || got[0].RequiredTools[0] != "deploy-cli" || got[0].RequiredTools[1] != "no-such-tool" {
		t.Fatalf("Match(deploy)[0].RequiredTools = %v, want exact registered slice", got[0].RequiredTools)
	}

	got = r.Match("code review")
	if len(got) != 1 || got[0].Name != "code-review" {
		t.Fatalf("Match(code review) = %v, want [code-review]", got)
	}
}
