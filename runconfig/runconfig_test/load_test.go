package runconfig_test

import (
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
)

// loadDoc runs Load on doc, failing the test on any error.
func loadDoc(t *testing.T, doc string) *runconfig.Definition {
	t.Helper()
	d, err := runconfig.Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

// oneStepDoc returns a minimal valid document whose single step binds
// the external tool named tool.
func oneStepDoc(tool string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "tool": "` + tool + `"}]},
		"tools": ["` + tool + `"]
	}`
}

// TestLoadAcceptsNormalToolName is the positive control for the
// whitespace-only tool name rejection: a normal name still loads, and
// Definition.Tools still names it.
func TestLoadAcceptsNormalToolName(t *testing.T) {
	d := loadDoc(t, oneStepDoc("grep"))
	found := false
	for _, name := range d.Tools {
		if name == "grep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Tools = %v, want it to contain %q", d.Tools, "grep")
	}
}

// TestLoadRoundTrip checks that every document section round-trips
// into the resolved Definition.
func TestLoadRoundTrip(t *testing.T) {
	d := loadDoc(t, `{
		"machine": {"initial": "idle", "transitions": [
			{"from": "idle", "to": "run", "trigger": "start"},
			{"from": "run", "to": "done", "trigger": "finish"}
		]},
		"plan": {
			"steps": [
				{"id": "a", "to": "run", "payload": "hello", "tool": "grep"},
				{"id": "b", "needs": ["a"], "to": "done", "when": "on_finished",
				 "retry": {"max_attempts": 3, "base_delay": "1ms", "max_delay": "5ms"}},
				{"id": "c", "needs": ["b"], "to": "done", "when": "on_failed",
				 "internal": "flow"}
			],
			"panels": []
		},
		"options": {"room": "platform-team", "ask_to": "human-1"},
		"tools": ["grep"]
	}`)

	if got := d.Machine.Initial(); got != machine.Status("idle") {
		t.Fatalf("initial = %q, want idle", got)
	}
	rows := d.Machine.Transitions()
	if len(rows) != 2 || rows[0].From != "idle" || rows[0].To != "run" || rows[0].Trigger != "start" {
		t.Fatalf("rows = %+v", rows)
	}
	if got := d.Options.Room; got != "platform-team" {
		t.Fatalf("room = %q", got)
	}
	if got := d.Options.AskTo; got != "human-1" {
		t.Fatalf("ask_to = %q", got)
	}
	if len(d.Tools) != 1 || d.Tools[0] != "grep" {
		t.Fatalf("tools = %+v", d.Tools)
	}

	steps := d.Plan.Steps()
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	a := steps[0]
	if a.ID != "a" || a.Payload != "hello" || a.To != "run" || a.When != flow.AdmissionOnSucceeded {
		t.Fatalf("step a = %+v", a)
	}
	b := steps[1]
	if len(b.Needs) != 1 || b.Needs[0] != "a" || b.When != flow.AdmissionOnFinished {
		t.Fatalf("step b = %+v", b)
	}
	if b.Retry == nil || b.Retry.MaxAttempts != 3 ||
		b.Retry.BaseDelay != time.Millisecond || b.Retry.MaxDelay != 5*time.Millisecond {
		t.Fatalf("step b retry = %+v", b.Retry)
	}
	if steps[2].When != flow.AdmissionOnFailed {
		t.Fatalf("step c when = %v", steps[2].When)
	}

	want := []runconfig.Binding{
		{Step: "a", Tool: "grep"},
		{Step: "c", Kind: runconfig.FlowKind, Internal: true},
	}
	if len(d.Bindings) != len(want) {
		t.Fatalf("bindings = %+v, want %+v", d.Bindings, want)
	}
	for i, w := range want {
		if d.Bindings[i] != w {
			t.Fatalf("binding %d = %+v, want %+v", i, d.Bindings[i], w)
		}
	}
}

// TestLoadPanels checks that panel waves survive the round trip.
func TestLoadPanels(t *testing.T) {
	d := loadDoc(t, `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {
			"steps": [
				{"id": "a", "to": "done"},
				{"id": "b", "to": "done", "needs": ["a"]}
			],
			"panels": [["a"]]
		},
		"tools": []
	}`)
	panels := d.Plan.Panels()
	if len(panels) != 1 || len(panels[0]) != 1 || panels[0][0] != "a" {
		t.Fatalf("panels = %+v", panels)
	}
}

// TestLoadSubAndLoop checks the nested sub plan and loop policy.
func TestLoadSubAndLoop(t *testing.T) {
	d := loadDoc(t, `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{
			"id": "outer", "to": "done",
			"loop": {"max": 2},
			"sub": {"steps": [{"id": "inner", "to": "done"}]}
		}]},
		"tools": []
	}`)
	steps := d.Plan.Steps()
	if len(steps) != 1 || steps[0].Loop == nil || steps[0].Loop.Max != 2 {
		t.Fatalf("steps = %+v", steps)
	}
	if steps[0].Sub == nil || len(steps[0].Sub.Steps()) != 1 ||
		steps[0].Sub.Steps()[0].ID != "inner" {
		t.Fatalf("sub = %+v", steps[0].Sub)
	}
	if len(d.Bindings) != 0 {
		t.Fatalf("bindings = %+v, want none", d.Bindings)
	}
}

// internalStepDoc returns a minimal valid document whose single step
// binds the internal kind named kind.
func internalStepDoc(kind string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "internal": "` + kind + `"}]},
		"tools": []
	}`
}

// TestLoadInternalKinds checks that every new Kind constant round-trips
// through Load into a matching, internal Binding.
func TestLoadInternalKinds(t *testing.T) {
	cases := []struct {
		name string
		kind runconfig.Kind
	}{
		{"flow", runconfig.FlowKind},
		{"ledger", runconfig.LedgerKind},
		{"memory", runconfig.MemoryKind},
		{"room", runconfig.RoomKind},
		{"scheduler", runconfig.SchedulerKind},
		{"heartbeat", runconfig.HeartbeatKind},
		{"discovery", runconfig.DiscoveryKind},
		{"trigger", runconfig.TriggerKind},
		{"channel", runconfig.ChannelKind},
		{"provider", runconfig.ProviderKind},
		{"providerregistry", runconfig.ProviderRegistryKind},
		{"astool", runconfig.AsToolKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadDoc(t, internalStepDoc(tc.name))
			if len(d.Bindings) != 1 {
				t.Fatalf("bindings = %+v, want one", d.Bindings)
			}
			b := d.Bindings[0]
			if b.Step != "s1" || !b.Internal || b.Kind != tc.kind {
				t.Fatalf("binding = %+v, want step s1 internal %q", b, tc.kind)
			}
		})
	}
}

// TestLoadOptionsBudget checks that a present options.budget round-trips
// into Options.Budget, and an absent one leaves it nil.
func TestLoadOptionsBudget(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		d := loadDoc(t, `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d"}]},
			"options": {"budget": {"max_bytes": 200000, "max_events": 500}},
			"tools": []
		}`)
		want := &contextbudget.Limits{MaxBytes: 200000, MaxEvents: 500}
		if d.Options.Budget == nil || *d.Options.Budget != *want {
			t.Fatalf("budget = %+v, want %+v", d.Options.Budget, want)
		}
	})
	t.Run("absent", func(t *testing.T) {
		d := loadDoc(t, `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d"}]},
			"options": {"room": "team"},
			"tools": []
		}`)
		if d.Options.Budget != nil {
			t.Fatalf("budget = %+v, want nil", d.Options.Budget)
		}
	})
}

// TestLoadOptionsTrace checks that options.trace builds a Tracer on
// true, and leaves it nil on false or absent.
func TestLoadOptionsTrace(t *testing.T) {
	doc := func(trace string) string {
		return `{
			"machine": {"initial": "q", "transitions": [
				{"from": "q", "to": "d", "trigger": "r"}
			]},
			"plan": {"steps": [{"id": "s", "to": "d"}]},
			"options": {` + trace + `},
			"tools": []
		}`
	}
	t.Run("true", func(t *testing.T) {
		d := loadDoc(t, doc(`"trace": true`))
		if d.Options.Tracer == nil {
			t.Fatal("Tracer = nil, want non-nil")
		}
	})
	t.Run("false", func(t *testing.T) {
		d := loadDoc(t, doc(`"trace": false`))
		if d.Options.Tracer != nil {
			t.Fatalf("Tracer = %+v, want nil", d.Options.Tracer)
		}
	})
	t.Run("absent", func(t *testing.T) {
		d := loadDoc(t, doc(`"room": "team"`))
		if d.Options.Tracer != nil {
			t.Fatalf("Tracer = %+v, want nil", d.Options.Tracer)
		}
	})
}

// TestLoadUnknownFieldsIgnored checks that extra JSON fields are
// ignored, matching envelope.Decode.
func TestLoadUnknownFieldsIgnored(t *testing.T) {
	d := loadDoc(t, `{
		"machine": {"initial": "q", "transitions": [
			{"from": "q", "to": "d", "trigger": "r", "guard": "no"}
		]},
		"plan": {"steps": [{"id": "s", "to": "d", "future": 1}]},
		"extra": true,
		"tools": []
	}`)
	if d.Plan == nil || d.Machine == nil {
		t.Fatal("nil plan or machine")
	}
}
