// Package machine implements the state-machine block:
// typed statuses, triggers, guards, actions, and the transition table.
//
// Map: status.go = Status; trigger.go = Trigger; transition.go =
// Guard, Action, Transition; inout.go = InOut; definition.go =
// Definition, New, Validate, Fire.
// Rationale: ../docs/plans/machine.md. Contribution rules: ../AGENTS.md.
package machine
