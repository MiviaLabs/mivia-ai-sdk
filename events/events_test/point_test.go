package events_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// TestPointValidate pins which Point values Validate accepts: the
// three named constants and nothing else. The zero value and any
// out-of-range value stay invalid.
func TestPointValidate(t *testing.T) {
	cases := []struct {
		name  string
		p     events.Point
		valid bool
	}{
		{"pre-tool", events.PointPreTool, true},
		{"post-tool", events.PointPostTool, true},
		{"stop", events.PointStop, true},
		{"zero value", events.Point(0), false},
		{"out of range", events.Point(99), false},
		{"negative", events.Point(-1), false},
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
		p    events.Point
		want string
	}{
		{"pre-tool", events.PointPreTool, "pre-tool"},
		{"post-tool", events.PointPostTool, "post-tool"},
		{"stop", events.PointStop, "stop"},
		{"zero value", events.Point(0), "unknown"},
		{"out of range", events.Point(99), "unknown"},
		{"negative", events.Point(-1), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
