// Package runconfig loads a JSON document into a validated
// agentrun runner and its tool set. The document names the machine
// rows, the plan steps and panels, string options, and the external
// tool set. The loader feeds flow.New, machine.New, and agentrun.New;
// it never re-runs their validation logic. See docs/plans/runconfig.md.
package runconfig
