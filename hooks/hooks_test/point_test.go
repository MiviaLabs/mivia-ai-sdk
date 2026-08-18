package hooks_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
)

// TestPointValidate pins which Point values Validate accepts: the
// three named constants and nothing else. The zero value and any
// out-of-range value stay invalid.
func TestPointValidate(t *testing.T) {
	cases := []struct {
		name  string
		p     hooks.Point
		valid bool
	}{
		{"pre-tool", hooks.PointPreTool, true},
		{"post-tool", hooks.PointPostTool, true},
		{"stop", hooks.PointStop, true},
		{"zero value", hooks.Point(0), false},
		{"out of range", hooks.Point(99), false},
		{"negative", hooks.Point(-1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.valid && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

// TestPointString pins the label each named constant renders and the
// "unknown" label every invalid value renders. No input panics.
func TestPointString(t *testing.T) {
	cases := []struct {
		name string
		p    hooks.Point
		want string
	}{
		{"pre-tool", hooks.PointPreTool, "pre-tool"},
		{"post-tool", hooks.PointPostTool, "post-tool"},
		{"stop", hooks.PointStop, "stop"},
		{"zero value", hooks.Point(0), "unknown"},
		{"out of range", hooks.Point(99), "unknown"},
		{"negative", hooks.Point(-1), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
