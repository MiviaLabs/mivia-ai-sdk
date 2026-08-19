package runconfig_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
)

// rejectCase is one ErrBadDocument shape and the fragment of its
// message that names the rule.
type rejectCase struct {
	name string
	doc  string
	want string
}

// baseDoc is a valid single-step document the cases mutate.
const baseDoc = `{
	"machine": {"initial": "q", "transitions": [
		{"from": "q", "to": "d", "trigger": "r"}
	]},
	"plan": {"steps": [{"id": "s", "to": "d", "tool": "grep"}]},
	"tools": ["grep"]
}`

// replace swaps old for new in the base document, failing the test
// when old is absent.
func replace(t *testing.T, old, new string) string {
	t.Helper()
	if !strings.Contains(baseDoc, old) {
		t.Fatalf("base document lacks %q", old)
	}
	return strings.Replace(baseDoc, old, new, 1)
}

// shapeCases lists the document-shape ErrBadDocument cases: JSON
// form, step binding rules, and the tools array.
func shapeCases(t *testing.T) []rejectCase {
	t.Helper()
	return []rejectCase{
		{
			name: "malformed json",
			doc:  `{"machine":`,
			want: "bad document",
		},
		{
			name: "non-object root",
			doc:  `[1,2]`,
			want: "bad document",
		},
		{
			name: "missing machine",
			doc:  `{"plan": {"steps": [{"id": "s"}]}}`,
			want: "machine and plan",
		},
		{
			name: "both bindings",
			doc:  replace(t, `"tool": "grep"}]`, `"tool": "grep", "internal": "flow"}]`),
			want: "both tool and internal",
		},
		{
			name: "empty id",
			doc:  replace(t, `"id": "s"`, `"id": ""`),
			want: "empty step id",
		},
		{
			name: "undeclared tool",
			doc:  replace(t, `"tools": ["grep"]`, `"tools": ["other"]`),
			want: "undeclared tool",
		},
		{
			name: "unknown internal",
			doc:  replace(t, `"tool": "grep"`, `"internal": "nonesuch"`),
			want: "unknown internal",
		},
		{
			name: "unknown when",
			doc:  replace(t, `"tool": "grep"`, `"when": "on_maybe", "tool": "grep"`),
			want: "unknown when",
		},
		{
			name: "duplicate tool",
			doc:  replace(t, `"tools": ["grep"]`, `"tools": ["grep", "grep"]`),
			want: "duplicate tool",
		},
		{
			name: "blank tool name",
			doc:  replace(t, `"tools": ["grep"]`, `"tools": [""]`),
			want: "blank tool name",
		},
		{
			name: "sub with declared tool",
			doc:  replace(t, `"tool": "grep"}]`, `"tool": "grep", "sub": {"steps": [{"id": "inner", "to": "d"}]}}]`),
			want: `step "s" sets sub beside tool or internal`,
		},
		{
			name: "sub with undeclared tool",
			doc:  replace(t, `"tool": "grep"}]`, `"tool": "other", "sub": {"steps": [{"id": "inner", "to": "d"}]}}]`),
			want: `step "s" sets sub beside tool or internal`,
		},
		{
			name: "sub with internal kind",
			doc:  replace(t, `"tool": "grep"}]`, `"internal": "flow", "sub": {"steps": [{"id": "inner", "to": "d"}]}}]`),
			want: `step "s" sets sub beside tool or internal`,
		},
		{
			name: "bad retry delay",
			doc:  replace(t, `"tool": "grep"`, `"retry": {"max_attempts": 1, "base_delay": "soon", "max_delay": "1ms"}, "tool": "grep"`),
			want: "base_delay",
		},
	}
}

// constructorCases lists the cases a typed constructor rejects and
// the loader forwards as ErrBadDocument.
func constructorCases(t *testing.T) []rejectCase {
	t.Helper()
	return []rejectCase{
		{
			name: "duplicate step id",
			doc:  replace(t, `[{"id": "s", "to": "d", "tool": "grep"}]`, `[{"id": "s", "to": "d", "tool": "grep"}, {"id": "s", "to": "d"}]`),
			want: "bad document",
		},
		{
			name: "missing dependency",
			doc:  replace(t, `"id": "s", "to": "d"`, `"id": "s", "needs": ["ghost"], "to": "d"`),
			want: "bad document",
		},
		{
			name: "empty machine table",
			doc:  replace(t, `{"from": "q", "to": "d", "trigger": "r"}`, ``),
			want: "bad document",
		},
		{
			name: "panel names unknown step",
			doc:  replace(t, `{"steps": [{"id": "s", "to": "d", "tool": "grep"}]}`, `{"steps": [{"id": "s", "to": "d", "tool": "grep"}], "panels": [["ghost"]]}`),
			want: "bad document",
		},
		{
			name: "invalid retry policy",
			doc:  replace(t, `"tool": "grep"`, `"retry": {"max_attempts": 0, "base_delay": "1ms", "max_delay": "1ms"}, "tool": "grep"`),
			want: "bad document",
		},
		{
			name: "nested flow rejection",
			doc:  replace(t, `"tool": "grep"`, `"sub": {"steps": [{"id": ""}]}`),
			want: "empty step id",
		},
	}
}

// rejectCases lists every ErrBadDocument shape.
func rejectCases(t *testing.T) []rejectCase {
	t.Helper()
	return append(shapeCases(t), constructorCases(t)...)
}

// TestLoadRejects checks every ErrBadDocument shape.
func TestLoadRejects(t *testing.T) {
	for _, tc := range rejectCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runconfig.Load([]byte(tc.doc))
			if err == nil {
				t.Fatal("Load succeeded, want rejection")
			}
			if !errors.Is(err, runconfig.ErrBadDocument) {
				t.Fatalf("err = %v, want ErrBadDocument", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestLoadSubAloneKeepsChildBindings is the positive control for the
// sub-beside-a-binding rejection: a step carrying sub alone loads, and
// its child step's tool binding reaches Definition.Bindings.
func TestLoadSubAloneKeepsChildBindings(t *testing.T) {
	doc := replace(t, `"tool": "grep"}]`, `"sub": {"steps": [{"id": "inner", "to": "d", "tool": "grep"}]}}]`)
	d, err := runconfig.Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(d.Bindings) != 1 {
		t.Fatalf("bindings = %+v, want one", d.Bindings)
	}
	if d.Bindings[0].Step != "inner" || d.Bindings[0].Tool != "grep" {
		t.Fatalf("binding = %+v, want inner bound to grep", d.Bindings[0])
	}
	if d.Plan.Steps()[0].Sub == nil {
		t.Fatal("sub plan = nil, want the child plan")
	}
}
