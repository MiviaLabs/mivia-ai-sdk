package e2e_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// e2eAgent builds a named agent over plan, failing the test on the
// first error.
func e2eAgent(t *testing.T, name string, plan *flow.Definition) *agent.Agent {
	t.Helper()
	a, err := e2e.NewAgent(name, plan)
	if err != nil {
		t.Fatalf("e2e.NewAgent: %v", err)
	}
	return a
}
