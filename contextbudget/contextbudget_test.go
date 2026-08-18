package contextbudget

import (
	"math"
	"strings"
	"testing"
)

func TestLimitsFits(t *testing.T) {
	tests := []struct {
		name   string
		limits Limits
		bytes  int
		events int
		want   bool
	}{
		{"both caps zero, large values fit", Limits{}, math.MaxInt, math.MaxInt, true},
		{"both caps zero, zero values fit", Limits{}, 0, 0, true},
		{"MaxBytes set, at cap", Limits{MaxBytes: 100}, 100, 999, true},
		{"MaxBytes set, under cap", Limits{MaxBytes: 100}, 99, 999, true},
		{"MaxBytes set, one over cap", Limits{MaxBytes: 100}, 101, 0, false},
		{"MaxEvents set, at cap", Limits{MaxEvents: 10}, 999, 10, true},
		{"MaxEvents set, under cap", Limits{MaxEvents: 10}, 999, 9, true},
		{"MaxEvents set, one over cap", Limits{MaxEvents: 10}, 0, 11, false},
		{"both set, both under", Limits{MaxBytes: 100, MaxEvents: 10}, 50, 5, true},
		{"both set, bytes over", Limits{MaxBytes: 100, MaxEvents: 10}, 101, 5, false},
		{"both set, events over", Limits{MaxBytes: 100, MaxEvents: 10}, 50, 11, false},
		{"negative bytes fits under positive cap", Limits{MaxBytes: 100}, -1, 0, true},
		{"negative events fits under positive cap", Limits{MaxEvents: 10}, 0, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.limits.Fits(tt.bytes, tt.events)
			if got != tt.want {
				t.Errorf("Fits(%d, %d) = %v, want %v", tt.bytes, tt.events, got, tt.want)
			}
		})
	}
}

func TestLimitsValidate(t *testing.T) {
	tests := []struct {
		name    string
		limits  Limits
		wantErr bool
		wantSub string
	}{
		{"zero-value Limits", Limits{}, false, ""},
		{"positive MaxBytes and MaxEvents", Limits{MaxBytes: 10, MaxEvents: 5}, false, ""},
		{"negative MaxBytes, zero MaxEvents", Limits{MaxBytes: -1}, true, "MaxBytes"},
		{"zero MaxBytes, negative MaxEvents", Limits{MaxEvents: -1}, true, "MaxEvents"},
		{"both fields negative", Limits{MaxBytes: -1, MaxEvents: -1}, true, "MaxBytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.limits.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() = nil, want error")
				}
				if !strings.Contains(err.Error(), tt.wantSub) {
					t.Errorf("Validate() error %q does not contain %q", err.Error(), tt.wantSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
