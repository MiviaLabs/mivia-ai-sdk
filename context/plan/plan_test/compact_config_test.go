package plan_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
)

// TestValidateRejectsNegativePercent pins the corrected error text:
// Validate rejects an out-of-range percent with "outside [0, 100]",
// not the stale "outside (0, 100]".
func TestValidateRejectsNegativePercent(t *testing.T) {
	err := plan.Compaction{TriggerPercent: -1}.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "outside [0, 100]") {
		t.Fatalf("Validate() error = %q, want it to contain %q", err.Error(), "outside [0, 100]")
	}
	if strings.Contains(err.Error(), "outside (0, 100]") {
		t.Fatalf("Validate() error = %q, still contains the stale bound text", err.Error())
	}
}

// TestValidateAcceptsZeroTriggerPercent is a positive control: the
// zero value passes as the package defaults, not as a rejected bound.
func TestValidateAcceptsZeroTriggerPercent(t *testing.T) {
	if err := (plan.Compaction{}).Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestValidateAcceptsZeroTargetPercent is a second positive control:
// TargetPercent zero passes independently of TriggerPercent's own
// bound check.
func TestValidateAcceptsZeroTargetPercent(t *testing.T) {
	c := plan.Compaction{TriggerPercent: 50, TargetPercent: 0}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestCompactionValidate(t *testing.T) {
	cases := []struct {
		name    string
		c       plan.Compaction
		wantErr bool
	}{
		{name: "zero value passes as defaults", c: plan.Compaction{}},
		{name: "trigger at one passes with a token override", c: plan.Compaction{TriggerPercent: 1, TargetTokens: 10}},
		{name: "trigger at hundred passes", c: plan.Compaction{TriggerPercent: 100}},
		{name: "trigger negative fails", c: plan.Compaction{TriggerPercent: -1}, wantErr: true},
		{name: "trigger over hundred fails", c: plan.Compaction{TriggerPercent: 101}, wantErr: true},
		{name: "target negative fails", c: plan.Compaction{TargetPercent: -1}, wantErr: true},
		{name: "target over hundred fails", c: plan.Compaction{TargetPercent: 101}, wantErr: true},
		{name: "target tokens negative fails", c: plan.Compaction{TargetTokens: -1}, wantErr: true},
		{name: "recent tail negative fails", c: plan.Compaction{RecentTail: -1}, wantErr: true},
		{name: "recent tail at max passes", c: plan.Compaction{RecentTail: plan.MaxRecentTail}},
		{
			name:    "recent tail over max fails",
			c:       plan.Compaction{RecentTail: plan.MaxRecentTail + 1},
			wantErr: true,
		},
		{name: "empty preserve name fails", c: plan.Compaction{PreserveNames: []string{"a", ""}}, wantErr: true},
		{
			name:    "duplicate preserve names fail",
			c:       plan.Compaction{PreserveNames: []string{"a", "a"}},
			wantErr: true,
		},
		{
			name:    "target percent at trigger fails in percent mode",
			c:       plan.Compaction{TriggerPercent: 50, TargetPercent: 50},
			wantErr: true,
		},
		{
			name:    "default target at trigger one fails in percent mode",
			c:       plan.Compaction{TriggerPercent: 1},
			wantErr: true,
		},
		{
			name: "target percent under trigger passes",
			c:    plan.Compaction{TriggerPercent: 50, TargetPercent: 49},
		},
		{
			name: "target percent at trigger passes in override mode",
			c:    plan.Compaction{TriggerPercent: 50, TargetPercent: 50, TargetTokens: 500},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.c.Validate()
			if c.wantErr && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestWindowValidateTargetTokens(t *testing.T) {
	cases := []struct {
		name    string
		w       plan.Window
		wantErr bool
	}{
		{
			name:    "target tokens at budget fails",
			w:       plan.Window{MaxTokens: 100, Compaction: plan.Compaction{TargetTokens: 100, TriggerPercent: 100}},
			wantErr: true,
		},
		{
			name:    "target tokens over budget fails",
			w:       plan.Window{MaxTokens: 100, Compaction: plan.Compaction{TargetTokens: 101, TriggerPercent: 100}},
			wantErr: true,
		},
		{
			name: "target tokens under budget passes",
			w: plan.Window{MaxTokens: 100,
				Compaction: plan.Compaction{TargetTokens: 99, TriggerPercent: 100}},
		},
		{
			name: "zero target tokens passes",
			w:    plan.Window{MaxTokens: 100},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.w.Validate()
			if c.wantErr && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestCompactTriggerTargetMath(t *testing.T) {
	cases := []struct {
		name        string
		w           plan.Window
		wantTrigger int
		wantTarget  int
	}{
		{
			name:        "defaults",
			w:           plan.Window{MaxTokens: 1000},
			wantTrigger: 1000,
			wantTarget:  100,
		},
		{
			name: "explicit percents",
			w: plan.Window{MaxTokens: 1000, Compaction: plan.Compaction{
				TriggerPercent: 40, TargetPercent: 25}},
			wantTrigger: 400,
			wantTarget:  250,
		},
		{
			name: "token override wins over percent",
			w: plan.Window{MaxTokens: 1000, Compaction: plan.Compaction{
				TriggerPercent: 40, TargetPercent: 25, TargetTokens: 123}},
			wantTrigger: 400,
			wantTarget:  123,
		},
		{
			name: "flooring",
			w: plan.Window{MaxTokens: 101, Compaction: plan.Compaction{
				TriggerPercent: 50, TargetPercent: 33}},
			wantTrigger: 50,
			wantTarget:  33,
		},
		{
			name: "reserve shrinks the budget",
			w: plan.Window{MaxTokens: 1000, Reserve: 100,
				Compaction: plan.Compaction{TriggerPercent: 50, TargetPercent: 10}},
			wantTrigger: 450,
			wantTarget:  90,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.w.CompactTrigger(); got != c.wantTrigger {
				t.Fatalf("CompactTrigger() = %d, want %d", got, c.wantTrigger)
			}
			if got := c.w.CompactTarget(); got != c.wantTarget {
				t.Fatalf("CompactTarget() = %d, want %d", got, c.wantTarget)
			}
		})
	}
}
