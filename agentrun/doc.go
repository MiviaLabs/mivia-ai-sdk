// Package agentrun wires an agent, a machine, and optional blocks
// into a runnable pipeline. One New validates the transition matrix,
// the tool names, the budget, and the option combinations before it
// builds anything. One Run method drives the wired run. Store, Ask,
// and Artifacts compose through the built ack chain, which runs each
// gated step's tool by ID and confirms its ack. See
// ../docs/plans/agentrun.md and ../docs/packages/agentrun.md.
package agentrun
