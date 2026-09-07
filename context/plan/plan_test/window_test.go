package plan_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
)

func TestWindowValidate(t *testing.T) {
	cases := []struct {
		name      string
		w         plan.Window
		wantError error
	}{
		{"valid", plan.Window{MaxTokens: 100, Reserve: 10}, nil},
		{"zero max tokens", plan.Window{MaxTokens: 0, Reserve: 0}, plan.ErrMaxTokensNotPositive},
		{"negative max tokens", plan.Window{MaxTokens: -1, Reserve: 0}, plan.ErrMaxTokensNotPositive},
		{"negative reserve", plan.Window{MaxTokens: 100, Reserve: -1}, plan.ErrReserveNegative},
		{"reserve equals max", plan.Window{MaxTokens: 100, Reserve: 100}, plan.ErrReserveTooLarge},
		{"reserve over max", plan.Window{MaxTokens: 100, Reserve: 200}, plan.ErrReserveTooLarge},
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
