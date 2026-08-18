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
	// AdmissionOnFinished admits a step when every need ended
	// OutcomeSucceeded or OutcomeSkipped. It is the zero value, so an
	// existing step keeps its behavior.
	AdmissionOnFinished Admission = iota
	// AdmissionOnSucceeded admits a step only when every need ended
	// OutcomeSucceeded. A skipped need skips this step.
	AdmissionOnSucceeded
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
// caller data. Sub nests a child workflow; when Sub is non-nil,
// Run ignores To and runs the child workflow to completion. A step
// with no Needs is a root. When sets the admission rule this step's
// needs must satisfy; the zero value is AdmissionOnFinished. Route
// makes this step a branch step: after it fires, Run calls Route to
// pick which of this step's direct dependents the run keeps. Retry
// bounds and paces repeated Fire attempts; a nil Retry keeps the
// single-attempt behavior. New rejects a non-nil Retry combined with
// a non-nil Sub or panel membership.
type Step struct {
	ID      string
	Needs   []string
	To      string
	Payload string
	Sub     *Definition
	When    Admission
	Route   Route
	Retry   *RetryPolicy
}
