package heartbeat_test

import (
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
)

// TestNew covers the timeout validation: positive, zero, and negative.
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr error
	}{
		{"positive timeout", time.Second, nil},
		{"zero timeout", 0, heartbeat.ErrNoTimeout},
		{"negative timeout", -time.Second, heartbeat.ErrNoTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := heartbeat.New(tc.timeout)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New(%v) error = %v, want %v", tc.timeout, err, tc.wantErr)
			}
			if tc.wantErr == nil && m == nil {
				t.Fatalf("New(%v) returned nil Monitor with nil error", tc.timeout)
			}
			if tc.wantErr != nil && m != nil {
				t.Fatalf("New(%v) returned non-nil Monitor with error %v", tc.timeout, err)
			}
		})
	}
}

// TestBeat covers id validation and the overwrite-vs-stale ordering rule.
func TestBeat(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("blank id", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("", base); !errors.Is(err, heartbeat.ErrNoID) {
			t.Fatalf("Beat(\"\") error = %v, want ErrNoID", err)
		}
	})
	t.Run("whitespace-only id", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("   ", base); !errors.Is(err, heartbeat.ErrNoID) {
			t.Fatalf("Beat(whitespace) error = %v, want ErrNoID", err)
		}
	})
	t.Run("fresh id", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("a", base); err != nil {
			t.Fatalf("Beat(fresh) error = %v, want nil", err)
		}
		if !m.Alive("a", base) {
			t.Fatalf("Alive(a) = false after fresh Beat, want true")
		}
	})
	t.Run("later at overwrites", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("a", base); err != nil {
			t.Fatalf("Beat(base) error = %v", err)
		}
		later := base.Add(time.Second)
		if err := m.Beat("a", later); err != nil {
			t.Fatalf("Beat(later) error = %v, want nil", err)
		}
		if !m.Alive("a", later) {
			t.Fatalf("Alive(a) = false at later, want true")
		}
	})
	t.Run("equal at overwrites", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("a", base); err != nil {
			t.Fatalf("Beat(base) error = %v", err)
		}
		if err := m.Beat("a", base); err != nil {
			t.Fatalf("Beat(equal) error = %v, want nil", err)
		}
	})
	t.Run("earlier at is stale", func(t *testing.T) {
		m, _ := heartbeat.New(time.Minute)
		if err := m.Beat("a", base); err != nil {
			t.Fatalf("Beat(base) error = %v", err)
		}
		earlier := base.Add(-time.Second)
		if err := m.Beat("a", earlier); !errors.Is(err, heartbeat.ErrStaleBeat) {
			t.Fatalf("Beat(earlier) error = %v, want ErrStaleBeat", err)
		}
		// The stored time stays at base; a beat one minute after base
		// stays within the timeout, proving the earlier at never landed.
		if !m.Alive("a", base.Add(59*time.Second)) {
			t.Fatalf("Alive(a) = false at base+59s, want true (stale beat must not overwrite)")
		}
	})
}

// TestAlive covers the never-beaten, within, boundary, past, and
// clock-skew cases.
func TestAlive(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Minute
	tests := []struct {
		name string
		beat *time.Time
		now  time.Time
		want bool
	}{
		{"never beat", nil, base, false},
		{"within timeout", ptr(base), base.Add(30 * time.Second), true},
		{"exactly at boundary", ptr(base), base.Add(timeout), true},
		{"just past timeout", ptr(base), base.Add(timeout + time.Nanosecond), false},
		{"clock skew: beat after now", ptr(base.Add(time.Hour)), base, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := heartbeat.New(timeout)
			if tc.beat != nil {
				if err := m.Beat("a", *tc.beat); err != nil {
					t.Fatalf("Beat error = %v", err)
				}
			}
			if got := m.Alive("a", tc.now); got != tc.want {
				t.Fatalf("Alive(a, %v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

// TestDead covers the empty set, a mixed alive/dead set, Dead after
// Forget, and the level-triggered repeat-call behavior.
func TestDead(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Minute

	t.Run("no ids tracked", func(t *testing.T) {
		m, _ := heartbeat.New(timeout)
		got := m.Dead(base)
		if len(got) != 0 {
			t.Fatalf("Dead() = %v, want empty", got)
		}
	})

	t.Run("mix of alive and dead ids sorted", func(t *testing.T) {
		m, _ := heartbeat.New(timeout)
		_ = m.Beat("zebra", base)
		_ = m.Beat("alive", base.Add(50*time.Second))
		_ = m.Beat("apple", base)
		now := base.Add(90 * time.Second)
		got := m.Dead(now)
		want := []string{"apple", "zebra"}
		if !equalSlices(got, want) {
			t.Fatalf("Dead(now) = %v, want %v", got, want)
		}
	})

	t.Run("Dead after Forget removes a dead id", func(t *testing.T) {
		m, _ := heartbeat.New(timeout)
		_ = m.Beat("a", base)
		_ = m.Beat("b", base)
		now := base.Add(90 * time.Second)
		m.Forget("a")
		got := m.Dead(now)
		want := []string{"b"}
		if !equalSlices(got, want) {
			t.Fatalf("Dead(now) after Forget = %v, want %v", got, want)
		}
	})

	t.Run("repeat calls are level-triggered", func(t *testing.T) {
		m, _ := heartbeat.New(timeout)
		_ = m.Beat("a", base)
		now := base.Add(90 * time.Second)
		first := m.Dead(now)
		second := m.Dead(now)
		if !equalSlices(first, second) {
			t.Fatalf("Dead(now) not stable across calls: %v vs %v", first, second)
		}
		if !equalSlices(first, []string{"a"}) {
			t.Fatalf("Dead(now) = %v, want [a]", first)
		}
	})
}

// TestForget covers a tracked id and an untracked id (no-op, no panic).
func TestForget(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m, _ := heartbeat.New(time.Minute)
	_ = m.Beat("a", base)

	t.Run("tracked id", func(t *testing.T) {
		m.Forget("a")
		if m.Alive("a", base) {
			t.Fatalf("Alive(a) = true after Forget, want false")
		}
	})

	t.Run("untracked id is a no-op", func(t *testing.T) {
		m.Forget("never-seen")
		if m.Alive("never-seen", base) {
			t.Fatalf("Alive(never-seen) = true, want false")
		}
	})
}

func ptr(t time.Time) *time.Time { return &t }

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
