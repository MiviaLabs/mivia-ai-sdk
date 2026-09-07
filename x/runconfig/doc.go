// Package runconfig loads a JSON document into a validated
// workflow/run runner and its tool set. The document names the machine
// rows, the plan steps and panels, string options, the external
// tool set, and the internal tools the loader builds. The loader
// feeds flow.New, machine.New, and run.New; it never re-runs
// their validation logic. See docs/history/runconfig.md.
package runconfig
