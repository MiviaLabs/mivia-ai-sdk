package flow

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Route picks the direct dependents a branch step's run keeps. It
// receives the branch step's post-fire status and record. It returns
// the IDs of the direct dependents to admit; every other direct
// dependent skips at once. A duplicate ID in the return collapses to
// one admission.
type Route func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error)

// Admission is the rule that admits a step after every one of its
// needs resolves.
type Admission int

const (
	// AdmissionOnSucceeded admits a step only when every need ended
	// OutcomeSucceeded. It is the zero value. A skipped need skips
	// this step, so route exclusion propagates to an excluded
	// branch's own dependents by default.
	AdmissionOnSucceeded Admission = iota
	// AdmissionOnFinished admits a step when every need ended
	// OutcomeSucceeded or OutcomeSkipped. It is the explicit opt-in
	// for skip tolerance, for join steps over optional branches: a
	// step below an excluded branch runs only by declaring this rule.
	AdmissionOnFinished
	// AdmissionOnFailed admits a step once every one of its needs is
	// terminal and at least one resolved OutcomeFailed. It is an
	// any-of rule over Needs, unlike the all-of rule the other two
	// values use. A step with this rule is a fallback: New rejects it
	// at the root (it would always admit) and inside a panel (a wave
	// shares one ctx across every member, with no per-member home for
	// the failure it would catch).
	AdmissionOnFailed
)

// Step is one node in a workflow graph.
// ID names the step. Needs lists the prerequisite step IDs.
// To holds the target status a later step binds. Payload carries
// caller data; PayloadFrom derives it from the live record. New
// rejects a step with both Payload and PayloadFrom set, and a
// PayloadFrom on a member of a panel of two or more members; a
// one-member panel keeps the field. Sub nests a child workflow; when
// Sub is non-nil, Run ignores To and runs the child workflow to
// completion. A step with no Needs is a root. When sets the admission
// rule this step's needs must satisfy; the zero value is
// AdmissionOnSucceeded, so a skipped need skips this step. Route
// makes this step a branch step: after it
// fires, Run calls Route to pick which of this step's direct
// dependents the run keeps. Retry bounds and paces repeated Fire
// attempts; a nil Retry keeps the single-attempt behavior. New rejects
// a non-nil Retry combined with a non-nil Sub or panel membership.
// Loop, when non-nil, runs Sub more than once, gated by
// LoopPolicy.Guard, before this step's own transition and Confirm
// fire; New rejects a non-nil Loop combined with a nil Sub or panel
// membership. A same-final child re-enters without a row; see
// LoopPolicy.
type Step struct {
	ID          string
	Needs       []string
	To          string
	Payload     string
	PayloadFrom func(rec machine.InOut) string
	Sub         *Definition
	When        Admission
	Route       Route
	Retry       *RetryPolicy
	Loop        *LoopPolicy
}
