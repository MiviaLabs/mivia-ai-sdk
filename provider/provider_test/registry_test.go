package provider_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestRegisterSentinels pins which sentinel Register returns for each
// rejection: a state conflict is ErrDuplicateName, an argument fault
// is ErrInvalidOptions.
func TestRegisterSentinels(t *testing.T) {
	for _, tc := range []struct {
		name      string
		first     string
		second    string
		duplicate bool
		invalid   bool
	}{
		{name: "duplicate name", first: "alpha", second: "alpha", duplicate: true},
		{name: "blank name", first: "alpha", second: "", invalid: true},
		{name: "whitespace name", first: "alpha", second: "   ", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := provider.NewRegistry()
			if err := r.Register(tc.first, &fakeCompleter{name: tc.first}); err != nil {
				t.Fatalf("Register(%q) error = %v, want nil", tc.first, err)
			}
			err := r.Register(tc.second, &fakeCompleter{name: "replacement"})
			if got := errors.Is(err, provider.ErrDuplicateName); got != tc.duplicate {
				t.Fatalf("errors.Is(%v, ErrDuplicateName) = %v, want %v", err, got, tc.duplicate)
			}
			if got := errors.Is(err, provider.ErrInvalidOptions); got != tc.invalid {
				t.Fatalf("errors.Is(%v, ErrInvalidOptions) = %v, want %v", err, got, tc.invalid)
			}
		})
	}
}

// TestRegister covers Register's accept path and its nil, blank-name,
// and duplicate-name rejection cases.
func TestRegister(t *testing.T) {
	t.Run("nil completer leaves the registry unchanged", func(t *testing.T) {
		r := provider.NewRegistry()
		err := r.Register("alpha", nil)
		if !errors.Is(err, provider.ErrInvalidOptions) {
			t.Fatalf("Register(nil) error = %v, want ErrInvalidOptions", err)
		}
		if !strings.Contains(err.Error(), "Completer") {
			t.Fatalf("Register(nil) error = %v, want it to name Completer", err)
		}
		if names := r.Names(); len(names) != 0 {
			t.Fatalf("Names() = %v, want empty after a rejected Register", names)
		}
	})
	t.Run("blank name", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			arg  string
		}{
			{name: "empty name", arg: ""},
			{name: "whitespace name", arg: "   "},
		} {
			t.Run(tc.name, func(t *testing.T) {
				r := provider.NewRegistry()
				err := r.Register(tc.arg, &fakeCompleter{name: "alpha"})
				if !errors.Is(err, provider.ErrInvalidOptions) {
					t.Fatalf("Register(%q) error = %v, want ErrInvalidOptions", tc.arg, err)
				}
				if !strings.Contains(err.Error(), "Name") {
					t.Fatalf("Register(%q) error = %v, want it to name Name", tc.arg, err)
				}
			})
		}
	})
	t.Run("valid name then Get resolves it", func(t *testing.T) {
		r := provider.NewRegistry()
		want := &fakeCompleter{name: "alpha"}
		if err := r.Register("alpha", want); err != nil {
			t.Fatalf("Register(alpha) error = %v, want nil", err)
		}
		got, ok := r.Get("alpha")
		if !ok {
			t.Fatal("Get(alpha) ok = false, want true")
		}
		if got != want {
			t.Fatalf("Get(alpha) = %v, want the registered completer", got)
		}
	})
	t.Run("duplicate name keeps the first completer", func(t *testing.T) {
		r := provider.NewRegistry()
		first := &fakeCompleter{name: "alpha"}
		if err := r.Register("alpha", first); err != nil {
			t.Fatalf("first Register(alpha) error = %v, want nil", err)
		}
		err := r.Register("alpha", &fakeCompleter{name: "replacement"})
		if !errors.Is(err, provider.ErrDuplicateName) {
			t.Fatalf("second Register(alpha) error = %v, want ErrDuplicateName", err)
		}
		if errors.Is(err, provider.ErrInvalidOptions) {
			t.Fatalf("second Register(alpha) error = %v, want it not to wrap ErrInvalidOptions", err)
		}
		got, ok := r.Get("alpha")
		if !ok || got != first {
			t.Fatalf("Get(alpha) = %v, %v, want the first completer and true", got, ok)
		}
	})
	t.Run("padded name registers under the raw key", func(t *testing.T) {
		r := provider.NewRegistry()
		if err := r.Register(" alpha", &fakeCompleter{name: "alpha"}); err != nil {
			t.Fatalf("Register(\" alpha\") error = %v, want nil", err)
		}
		if _, ok := r.Get("alpha"); ok {
			t.Fatal("Get(alpha) ok = true, want false: Register must not trim before storing")
		}
		if _, ok := r.Get(" alpha"); !ok {
			t.Fatal("Get(\" alpha\") ok = false, want true: Register stores the raw key")
		}
	})
}

// TestGet covers the present and absent lookup paths.
func TestGet(t *testing.T) {
	r := provider.NewRegistry()
	want := &fakeCompleter{name: "alpha"}
	if err := r.Register("alpha", want); err != nil {
		t.Fatalf("Register(alpha) error = %v, want nil", err)
	}
	t.Run("registered name", func(t *testing.T) {
		got, ok := r.Get("alpha")
		if !ok {
			t.Fatal("Get(alpha) ok = false, want true")
		}
		if got != want {
			t.Fatalf("Get(alpha) = %v, want the registered completer", got)
		}
	})
	t.Run("unregistered name", func(t *testing.T) {
		got, ok := r.Get("missing")
		if ok || got != nil {
			t.Fatalf("Get(missing) = %v, %v, want nil, false", got, ok)
		}
	})
}

// TestNames covers the set, not the order: three registrations list
// all three names.
func TestNames(t *testing.T) {
	r := provider.NewRegistry()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := r.Register(name, &fakeCompleter{name: name}); err != nil {
			t.Fatalf("Register(%s) error = %v, want nil", name, err)
		}
	}
	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("Names() len = %d, want 3", len(names))
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !set[want] {
			t.Fatalf("Names() = %v, lacks %s", names, want)
		}
	}
}
