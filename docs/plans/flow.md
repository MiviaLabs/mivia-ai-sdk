# Plan: flow

Status: future. No code yet. Rationale in docs/research-a2a.md.

## Goal

Run a small declarative workflow over envelope messages. A workflow is
a list of steps with dependencies, nothing more.

## Scope

Inside: a Step struct (id, needs, target, payload). A runner that
executes steps in topological levels. Ack confirmation gates each
step. The thread chain records the audit trail. Outside: retries,
compensation, scheduling, persistence, dynamic replanning. Sequential
execution first; parallel levels only if a real task asks.

## API

Proposed shape, subject to plan review:

- `type Step struct { ID string; Needs []string; To string; Payload string }`
- `Run(ctx, r *room.Room, steps []Step) error`

A new row in policy/layers.json: flow imports envelope and room.

## Tests

Topological order on a diamond DAG. Cycle detection. A step that
fails its ack stops the run. The audit thread verifies with
VerifyThread after the run.

## Verification

`make verify`. Plans and design doc update in the same change.
