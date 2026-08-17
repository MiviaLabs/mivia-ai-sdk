package flow

// Step is one node in a workflow graph.
// ID names the step. Needs lists the prerequisite step IDs.
// To holds the target status a later step binds. Payload carries
// caller data. Sub nests a child workflow; when Sub is non-nil,
// Run ignores To and runs the child workflow to completion. A step
// with no Needs is a root.
type Step struct {
	ID      string
	Needs   []string
	To      string
	Payload string
	Sub     *Definition
}
