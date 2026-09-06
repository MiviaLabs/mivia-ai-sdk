package runconfig_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// errStubTool names the failing stub's deliberate failure. A run
// error carrying it proves the stub, not a document-built tool,
// served the step.
var errStubTool = errors.New("runconfig_test: stub tool failed")

// stubFailTool is an internal tool stub whose Run always fails.
type stubFailTool struct{ name string }

// Name returns the registry name.
func (s stubFailTool) Name() string { return s.name }

// Run fails with errStubTool.
func (s stubFailTool) Run(context.Context, tools.InOut) (tools.Out, error) {
	return tools.Out{}, errStubTool
}

// docWithInternal returns a document whose single step binds the
// internal Kind named kind. The internal section declares kind with
// the config object cfg, and the step carries the pre-escaped JSON
// payload.
func docWithInternal(kind, cfg, payload string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done",
			"payload": "` + payload + `", "internal": "` + kind + `"}]},
		"internal": {"` + kind + `": ` + cfg + `},
		"tools": []
	}`
}

// docWithSection returns a document with one step bound to the
// declared external tool grep, plus the raw internal section JSON, so
// a rejection row isolates one section key.
func docWithSection(section string) string {
	return `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "tool": "grep"}]},
		"internal": ` + section + `,
		"tools": ["grep"]
	}`
}

// TestLoadBuildsInternalTools proves each wireable Kind loads from a
// document declaration and builds a Runner. No Blocks.Set call runs
// anywhere in this test. The stray-field row pins the unknown-field
// rule: a config carrying an extra key still loads.
func TestLoadBuildsInternalTools(t *testing.T) {
	cases := []struct {
		name string
		kind string
		cfg  string
	}{
		{"discovery", "discovery", `{}`},
		{"flow", "flow", `{}`},
		{"heartbeat", "heartbeat", `{"timeout": "30s"}`},
		{"ledger", "ledger", `{"actor": "alpha", "lease": "1m"}`},
		{"memory", "memory", `{"max_bytes": 1024}`},
		{"room", "room", `{"id": "room-1", "founder": "f1", "actor": "a1"}`},
		{"heartbeat ignores unknown fields", "heartbeat", `{"timeout": "30s", "max_bytes": 999}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadDoc(t, docWithInternal(tc.kind, tc.cfg, ""))
			d.Options.Agent = agentOver(t, d)
			runner, err := d.Runner()
			if err != nil {
				t.Fatalf("Runner: %v", err)
			}
			if runner == nil {
				t.Fatal("Runner = nil, want a built Runner")
			}
		})
	}
}

// TestLoadEmptyInternalSection pins the present-but-empty edge: a
// section with no entries loads and builds like an absent one.
func TestLoadEmptyInternalSection(t *testing.T) {
	d := loadDoc(t, `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "tool": "grep"}]},
		"internal": {},
		"tools": ["grep"]
	}`)
	if err := d.External.Add(stubTool{name: "grep"}); err != nil {
		t.Fatalf("External.Add: %v", err)
	}
	d.Options.Agent = agentOver(t, d)
	if _, err := d.Runner(); err != nil {
		t.Fatalf("Runner: %v", err)
	}
}

// TestPartialInternalResolution proves a partially filled Blocks
// composes with document-built tools. Row one: a document-built entry
// never masks an undeclared sibling Kind. Row two: a caller stub over
// memory serves only its own step, and the document-built heartbeat
// tool serves beat. The reached status is done because flow fires the
// step's transition before the confirm-time tool call.
func TestPartialInternalResolution(t *testing.T) {
	t.Run("undeclared sibling kind still fails", func(t *testing.T) {
		doc := `{
			"machine": {"initial": "queued", "transitions": [
				{"from": "queued", "to": "done", "trigger": "run"}
			]},
			"plan": {"steps": [{"id": "s1", "to": "done", "internal": "memory"}]},
			"internal": {"heartbeat": {"timeout": "30s"}},
			"tools": []
		}`
		d := loadDoc(t, doc)
		d.Options.Agent = agentOver(t, d)
		_, err := d.Runner()
		if !errors.Is(err, runconfig.ErrUnknownInternal) {
			t.Fatalf("err = %v, want ErrUnknownInternal", err)
		}
		if !strings.Contains(err.Error(), "memory") {
			t.Fatalf("err = %v, want it to name memory", err)
		}
	})
	t.Run("caller stub over memory only", func(t *testing.T) {
		doc := `{
			"machine": {"initial": "queued", "transitions": [
				{"from": "queued", "to": "mid", "trigger": "run"},
				{"from": "mid", "to": "done", "trigger": "finish"}
			]},
			"plan": {"steps": [
				{"id": "beat", "to": "mid",
				 "payload": "{\"op\":\"beat\",\"id\":\"w1\"}", "internal": "heartbeat"},
				{"id": "mem", "needs": ["beat"], "to": "done",
				 "payload": "{\"op\":\"put\",\"data\":\"x\"}", "internal": "memory"}
			]},
			"internal": {"heartbeat": {"timeout": "30s"}, "memory": {"max_bytes": 1024}},
			"tools": []
		}`
		d := loadDoc(t, doc)
		d.Blocks.Set(runconfig.MemoryKind, stubFailTool{name: "mem"})
		d.Options.Agent = agentOver(t, d)
		runner, err := d.Runner()
		if err != nil {
			t.Fatalf("Runner: %v", err)
		}
		status, _, err := runner.Run(context.Background(), "thread-partial", machine.InOut{})
		if !errors.Is(err, errStubTool) {
			t.Fatalf("err = %v, want errStubTool", err)
		}
		if !strings.Contains(err.Error(), `"mem"`) {
			t.Fatalf("err = %v, want it to name step mem", err)
		}
		if status != "done" {
			t.Fatalf("status = %q, want done: the step transition fires before the tool call", status)
		}
	})
}

// TestLoadRejectsCallerBuiltInternal proves a document declaring a
// caller-built Kind fails Load with ErrBadDocument wrapping
// ErrCallerBuilt, naming the key.
func TestLoadRejectsCallerBuiltInternal(t *testing.T) {
	cases := []struct {
		name string
		kind string
	}{
		{"astool", "astool"},
		{"channel", "channel"},
		{"provider", "provider"},
		{"providerregistry", "providerregistry"},
		{"scheduler", "scheduler"},
		{"trigger", "trigger"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runconfig.Load([]byte(docWithInternal(tc.kind, `{}`, "")))
			if !errors.Is(err, runconfig.ErrBadDocument) {
				t.Fatalf("err = %v, want ErrBadDocument", err)
			}
			if !errors.Is(err, runconfig.ErrCallerBuilt) {
				t.Fatalf("err = %v, want ErrCallerBuilt", err)
			}
			if !strings.Contains(err.Error(), tc.kind) {
				t.Fatalf("err = %v, want it to name %q", err, tc.kind)
			}
		})
	}
}

// TestLoadRejectsBadInternalConfig proves the loader validates each
// Kind's config and forwards leaf constructor rejections. Each row
// asserts ErrBadDocument and one naming fragment.
func TestLoadRejectsBadInternalConfig(t *testing.T) {
	cases := []struct {
		name    string
		section string
		frag    string
	}{
		{"unknown key", `{"nosuchkind": {}}`, `unknown internal "nosuchkind"`},
		{"bad heartbeat duration", `{"heartbeat": {"timeout": "soon"}}`, `internal "heartbeat" config "timeout"`},
		{"zero heartbeat timeout", `{"heartbeat": {"timeout": "0s"}}`, `heartbeat: timeout must be positive`},
		{"zero memory max_bytes", `{"memory": {"max_bytes": 0}}`, `memory: maxBytes must be positive`},
		{"blank room id", `{"room": {"id": "", "founder": "f1", "actor": "a1"}}`, `room id is required`},
		{"blank room founder", `{"room": {"id": "r1", "founder": "", "actor": "a1"}}`, `founder is required`},
		{"blank room actor", `{"room": {"id": "r1", "founder": "f1", "actor": ""}}`, `internal "room": blank actor`},
		{"blank ledger actor", `{"ledger": {"actor": "", "lease": "1m"}}`, `internal "ledger": blank actor`},
		{"bad ledger lease string", `{"ledger": {"actor": "alpha", "lease": "soon"}}`, `internal "ledger" config "lease"`},
		{"zero ledger lease", `{"ledger": {"actor": "alpha", "lease": "0s"}}`, `internal "ledger": lease must be positive`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runconfig.Load([]byte(docWithSection(tc.section)))
			if !errors.Is(err, runconfig.ErrBadDocument) {
				t.Fatalf("err = %v, want ErrBadDocument", err)
			}
			if !strings.Contains(err.Error(), tc.frag) {
				t.Fatalf("err = %v, want it to name %s", err, tc.frag)
			}
		})
	}
}

// TestInternalToolsRunThroughRunner proves each command Kind answers
// through a real Runner.Run. Each row's step payload carries one
// valid JSON command, and the run completes with status done.
func TestInternalToolsRunThroughRunner(t *testing.T) {
	cases := []struct {
		name string
		kind string
		cfg  string
		cmd  string
	}{
		{"discovery", "discovery", `{}`,
			`{\"op\":\"match\",\"card\":\"{\\\"name\\\":\\\"c1\\\",\\\"capabilities\\\":[\\\"cap\\\"]}\",\"need\":\"cap\"}`},
		{"heartbeat", "heartbeat", `{"timeout": "30s"}`,
			`{\"op\":\"beat\",\"id\":\"w1\"}`},
		{"ledger", "ledger", `{"actor": "alpha", "lease": "1m"}`,
			`{\"op\":\"state\",\"key\":\"k1\"}`},
		{"memory", "memory", `{"max_bytes": 1024}`,
			`{\"op\":\"put\",\"data\":\"x\"}`},
		{"room", "room", `{"id": "room-1", "founder": "f1", "actor": "a1"}`,
			`{\"op\":\"ismember\",\"id\":\"nobody\"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := loadDoc(t, docWithInternal(tc.kind, tc.cfg, tc.cmd))
			d.Options.Agent = agentOver(t, d)
			runner, err := d.Runner()
			if err != nil {
				t.Fatalf("Runner: %v", err)
			}
			status, _, err := runner.Run(context.Background(), "thread-internal", machine.InOut{})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if status != "done" {
				t.Fatalf("status = %q, want done", status)
			}
		})
	}
}

// TestInternalFlowToolRunsOwnPlan proves the document-built flow tool
// drives the document's own plan and machine to the final status. The
// flow walk never runs step tools, so the flow-bound step cannot
// recurse.
func TestInternalFlowToolRunsOwnPlan(t *testing.T) {
	doc := `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "walk", "to": "done", "payload": "walk",
			"internal": "flow"}]},
		"internal": {"flow": {}},
		"tools": []
	}`
	d := loadDoc(t, doc)
	d.Options.Agent = agentOver(t, d)
	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-flow-internal", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
}

// TestCallerSetOverridesDocumentKind proves Blocks.Set after Load
// replaces a document-built tool for that Kind: the run fails with
// the stub's error, so the caller's tool served the step.
func TestCallerSetOverridesDocumentKind(t *testing.T) {
	d := loadDoc(t, docWithInternal("memory", `{"max_bytes": 1024}`,
		`{\"op\":\"put\",\"data\":\"x\"}`))
	d.Blocks.Set(runconfig.MemoryKind, stubFailTool{name: "s1"})
	d.Options.Agent = agentOver(t, d)
	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	_, _, err = runner.Run(context.Background(), "thread-override", machine.InOut{})
	if !errors.Is(err, errStubTool) {
		t.Fatalf("err = %v, want errStubTool", err)
	}
}
