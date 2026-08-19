package contextplan_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
)

func TestWindowValidate(t *testing.T) {
	cases := []struct {
		name    string
		w       contextplan.Window
		wantErr bool
	}{
		{"valid", contextplan.Window{MaxTokens: 100, Reserve: 10}, false},
		{"zero max tokens", contextplan.Window{MaxTokens: 0, Reserve: 0}, true},
		{"negative max tokens", contextplan.Window{MaxTokens: -1, Reserve: 0}, true},
		{"negative reserve", contextplan.Window{MaxTokens: 100, Reserve: -1}, true},
		{"reserve equals max", contextplan.Window{MaxTokens: 100, Reserve: 100}, true},
		{"reserve over max", contextplan.Window{MaxTokens: 100, Reserve: 200}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate accepted an invalid Window")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate rejected a valid Window: %v", err)
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
