package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// allowHandler returns a HookHandler that allows and records nothing.
func allowHandler() events.HookHandler {
	return func(context.Context, any) (bool, error) { return true, nil }
}

// TestAddInvalidPoint pins Add's first rejection: an invalid Point
// returns its Validate error and registers nothing. The zero value
// and the out-of-range value both stay invalid; an external test
// package cannot name pointUnset, so Point(0) stands in for it.
func TestAddInvalidPoint(t *testing.T) {
	for _, p := range []events.Point{0, 99} {
		r := events.NewRegistry()
		err := r.Add(p, "name", allowHandler())
		if err == nil {
			t.Fatalf("Add(Point(%d)) = nil, want the Validate error", int(p))
		}
		if ok := r.Remove(events.PointPreTool, "name"); ok {
			t.Fatal("Add registered a handler despite an invalid Point")
		}
	}
}

// TestAddBlankName pins ErrBlankName for a name that is empty after
// strings.TrimSpace.
func TestAddBlankName(t *testing.T) {
	r := events.NewRegistry()
	for _, name := range []string{"", "   ", "\t"} {
		err := r.Add(events.PointPreTool, name, allowHandler())
		if !errors.Is(err, events.ErrBlankName) {
			t.Fatalf("Add(%q) = %v, want ErrBlankName", name, err)
		}
	}
}

// TestAddNilHandler pins ErrNilHandler.
func TestAddNilHandler(t *testing.T) {
	r := events.NewRegistry()
	err := r.Add(events.PointPreTool, "name", nil)
	if !errors.Is(err, events.ErrNilHandler) {
		t.Fatalf("Add(nil) = %v, want ErrNilHandler", err)
	}
}

// TestAddRejectionOrder pins the order Add's doc comment promises:
// the Point check first, then the blank name, then the nil handler.
// This distinguishes Add from an implementation that checks in
// another order.
func TestAddRejectionOrder(t *testing.T) {
	cases := []struct {
		name string
		p    events.Point
		h    events.HookHandler
		want error
	}{
		{"point before name", 0, nil, nil},
		{"name before handler", events.PointPreTool, nil, events.ErrBlankName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := events.NewRegistry()
			err := r.Add(tc.p, "   ", tc.h)
			if tc.want == nil {
				if err == nil {
					t.Fatal("Add = nil, want the Validate error")
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Add = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestAddDuplicateNameAtSamePoint pins ErrDuplicateName. The first
// registration stays the live one; Add never replaces an entry.
func TestAddDuplicateNameAtSamePoint(t *testing.T) {
	r := events.NewRegistry()
	if err := r.Add(events.PointStop, "audit-log", allowHandler()); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err := r.Add(events.PointStop, "audit-log", allowHandler())
	if !errors.Is(err, events.ErrDuplicateName) {
		t.Fatalf("second Add = %v, want ErrDuplicateName", err)
	}
}

// TestAddSameNameAtTwoPoints pins that name scopes to one Point, not
// to the whole Registry: one label registers at two different points
// and both Fire.
func TestAddSameNameAtTwoPoints(t *testing.T) {
	r := events.NewRegistry()
	if err := r.Add(events.PointPreTool, "audit-log", allowHandler()); err != nil {
		t.Fatalf("Add(pre-tool): %v", err)
	}
	if err := r.Add(events.PointPostTool, "audit-log", allowHandler()); err != nil {
		t.Fatalf("Add(post-tool): %v", err)
	}
	if err := r.Fire(context.Background(), events.PointPreTool, nil); err != nil {
		t.Fatalf("Fire(pre-tool): %v", err)
	}
	if err := r.Fire(context.Background(), events.PointPostTool, nil); err != nil {
		t.Fatalf("Fire(post-tool): %v", err)
	}
}

// TestRemove pins Remove's contract: true for a present (point, name)
// pair, false for an absent one, and false again on a second call.
func TestRemove(t *testing.T) {
	r := events.NewRegistry()
	if err := r.Add(events.PointStop, "present", allowHandler()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ok := r.Remove(events.PointStop, "present"); !ok {
		t.Fatal("Remove(present) = false, want true")
	}
	if ok := r.Remove(events.PointStop, "present"); ok {
		t.Fatal("second Remove(present) = true, want false")
	}
	if ok := r.Remove(events.PointStop, "never-added"); ok {
		t.Fatal("Remove(absent) = true, want false")
	}
	if ok := r.Remove(events.PointPreTool, "present"); ok {
		t.Fatal("Remove(wrong point) = true, want false")
	}
}
