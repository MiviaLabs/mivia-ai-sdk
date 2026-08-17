// Package machine implements the state-machine block:
// typed statuses, triggers, guards, and the transition table.
//
// Map: status.go = Status; trigger.go = Trigger; transition.go =
// Guard, Transition; definition.go = Definition, New, Validate.
// Rationale: ../docs/plans/machine.md. Contribution rules: ../AGENTS.md.
package machine
