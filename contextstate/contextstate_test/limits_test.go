package contextstate_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

func TestLimitsValidateAccepts(t *testing.T) {
	accept := []struct {
		name   string
		limits contextstate.Limits
	}{
		{"zero value", contextstate.Limits{}},
		{"checkpoint bytes", contextstate.Limits{CheckpointBytes: 1}},
		{"commit events", contextstate.Limits{CommitEvents: 1}},
		{"commit event bytes", contextstate.Limits{CommitEventBytes: 1}},
		{"all positive", contextstate.Limits{CheckpointBytes: 1, CommitEvents: 2, CommitEventBytes: 3}},
	}
	for _, tc := range accept {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.limits.Validate(); err != nil {
				t.Fatalf("Validate rejected a zero-or-positive Limits: %v", err)
			}
		})
	}
}

func TestLimitsValidateRejectsNegative(t *testing.T) {
	reject := []struct {
		name      string
		limits    contextstate.Limits
		wantField string
	}{
		{"checkpoint bytes negative", contextstate.Limits{CheckpointBytes: -1}, "limits.checkpoint_bytes"},
		{"commit events negative", contextstate.Limits{CommitEvents: -1}, "limits.commit_events"},
		{"commit event bytes negative", contextstate.Limits{CommitEventBytes: -1}, "limits.commit_event_bytes"},
		{"checkpoint checked first", contextstate.Limits{CheckpointBytes: -1, CommitEvents: -1}, "limits.checkpoint_bytes"},
		{"events before bytes", contextstate.Limits{CommitEvents: -1, CommitEventBytes: -1}, "limits.commit_events"},
		{"all negative names checkpoint", contextstate.Limits{CheckpointBytes: -1, CommitEvents: -1, CommitEventBytes: -1}, "limits.checkpoint_bytes"},
	}
	for _, tc := range reject {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.limits.Validate()
			if err == nil {
				t.Fatal("Validate accepted a negative Limits")
			}
			if !errors.Is(err, contextstate.ErrInvalidRecord) {
				t.Fatalf("error %v does not wrap ErrInvalidRecord", err)
			}
			var ve *contextstate.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error %v is not a ValidationError", err)
			}
			if ve.Field != tc.wantField {
				t.Fatalf("Field = %q, want %q", ve.Field, tc.wantField)
			}
		})
	}
}
