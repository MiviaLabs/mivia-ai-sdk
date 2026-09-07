package contextplan_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
)

func TestWindowValidate(t *testing.T) {
	cases := []struct {
		name      string
		w         contextplan.Window
		wantError error
	}{
		{"valid", contextplan.Window{MaxTokens: 100, Reserve: 10}, nil},
		{"zero max tokens", contextplan.Window{MaxTokens: 0, Reserve: 0}, contextplan.ErrMaxTokensNotPositive},
		{"negative max tokens", contextplan.Window{MaxTokens: -1, Reserve: 0}, contextplan.ErrMaxTokensNotPositive},
		{"negative reserve", contextplan.Window{MaxTokens: 100, Reserve: -1}, contextplan.ErrReserveNegative},
		{"reserve equals max", contextplan.Window{MaxTokens: 100, Reserve: 100}, contextplan.ErrReserveTooLarge},
		{"reserve over max", contextplan.Window{MaxTokens: 100, Reserve: 200}, contextplan.ErrReserveTooLarge},
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
	valid := contextplan.Window{MaxTokens: 100, Reserve: 30}
	if got := valid.Budget(); got != 70 {
		t.Fatalf("Budget() = %d, want 70", got)
	}
	invalid := contextplan.Window{MaxTokens: 0, Reserve: 0}
	if got := invalid.Budget(); got != 0 {
		t.Fatalf("Budget() on an invalid Window = %d, want 0", got)
	}
}
