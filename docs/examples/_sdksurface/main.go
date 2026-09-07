// Command sdksurface exercises the exported SDK symbols that carry no
// in-repo caller: the surface a public SDK ships for external
// application code. Each surface function builds its dependencies,
// calls the symbols the way an external application would, and checks
// one observable result. The program prints "final: OK" when every
// surface call succeeds. See docs/examples/ for the full guided
// versions of the flows this file compresses.
package main

import (
	"fmt"
)

func main() {
	checks := []func() error{
		a2aSurface,
		agentloopSurface,
		runSurface,
		channelSurface,
		planSurface,
		envelopeSurface,
		flowSurface,
		ctxProbeSurface,
		heartbeatSurface,
		e2eSurface,
		ledgerSurface,
		machineSurface,
		mcpSurface,
		memorySurface,
		providerSurface,
		roomSurface,
		subagentSurface,
		workspaceSurface,
	}
	for i, check := range checks {
		if err := check(); err != nil {
			fmt.Println("surface", i, ":", err)
			return
		}
	}
	fmt.Println("final: OK")
}
