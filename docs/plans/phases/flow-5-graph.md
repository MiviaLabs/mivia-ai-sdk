# Plan: flow phase 5 — step graph and validation

Adds the step graph and its validation to the flow package. Kahn's
algorithm sorts and detects cycles. This phase starts in parallel with
machine phase 4.

## Goal

A Definition of steps, panels, and the graph validation. New rejects a
graph with a cycle and groups nodes into topological levels.

## Scope

Inside: the Step, Panel, and Definition types; New; and Validate.
Outside: execution and chaining. Validation only, no runner yet.

## API

- `type Step struct { ID string; Needs []string; To string; Payload string }`
- `type Panel []string`
- `type Definition struct` holding steps, panels, and levels.
- `New(steps []Step, panels []Panel) (*Definition, error)`
- `(*Definition).Validate() error`

New runs Kahn's algorithm. It returns the topological order and the
wave levels. A leftover node means a cycle, so New returns an error.
Panels reference step IDs; a panel that names an unknown step fails.

## TDD

Write the diamond test first. Then the cycle test and the fan-in test.
Build New and Validate against those failing tests.

## Tests

Unit tables for graph shape:

- a diamond DAG sorts in the right order.
- fan-in and fan-out produce the right wave levels.
- a cycle returns an error.
- an unknown step in Needs returns an error.
- a panel that names a missing step returns an error.
- Validate accepts a New-built definition and rejects a bad one.

## Verification

`make verify`. The coverage floor for the flow package is 85 percent.
The flow import of machine lands in policy/layers.json with this phase.
