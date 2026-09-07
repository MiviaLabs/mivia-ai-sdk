package plan_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
)

func TestWindowValidate(t *testing.T) {
	cases := []struct {
		name      string
		w         plan.Window
		wantError error
		wantSub   string
	}{
		{name: "valid", w: plan.Window{MaxTokens: 100, Reserve: 10}},
		{
			name:      "zero max tokens",
			w:         plan.Window{MaxTokens: 0, Reserve: 0},
			wantError: plan.ErrMaxTokensNotPositive,
		},
		{
			name:      "negative max tokens",
			w:         plan.Window{MaxTokens: -1, Reserve: 0},
			wantError: plan.ErrMaxTokensNotPositive,
		},
		{
			name:      "negative reserve",
			w:         plan.Window{MaxTokens: 100, Reserve: -1},
			wantError: plan.ErrInvalidOptions,
			wantSub:   "Reserve",
		},
		{
			name:      "reserve equals max",
			w:         plan.Window{MaxTokens: 100, Reserve: 100},
			wantError: plan.ErrInvalidOptions,
			wantSub:   "Reserve",
		},
		{
			name:      "reserve over max",
			w:         plan.Window{MaxTokens: 100, Reserve: 200},
			wantError: plan.ErrInvalidOptions,
			wantSub:   "Reserve",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if tc.wantError == nil {
				if err != nil {
					t.Fatalf("Validate rejected a valid Window: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("Validate error = %v, want %v", err, tc.wantError)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Validate error = %q, want it to contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestWindowBudget(t *testing.T) {
	valid := plan.Window{MaxTokens: 100, Reserve: 30}
	if got := valid.Budget(); got != 70 {
		t.Fatalf("Budget() = %d, want 70", got)
	}
	invalid := plan.Window{MaxTokens: 0, Reserve: 0}
	if got := invalid.Budget(); got != 0 {
		t.Fatalf("Budget() on an invalid Window = %d, want 0", got)
	}
}
