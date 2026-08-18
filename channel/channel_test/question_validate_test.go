// Package channel_test holds channel's external test suite.
package channel_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// TestQuestionValidate covers red-green cases for Question.Validate:
// an empty ID, an empty Recipient, an empty Payload, a whitespace-only
// ID (proving the trim rule), and a fully populated value.
func TestQuestionValidate(t *testing.T) {
	cases := []struct {
		name    string
		q       channel.Question
		wantErr error
	}{
		{
			name:    "empty id",
			q:       channel.Question{ID: "", Recipient: "human", Payload: "hi"},
			wantErr: channel.ErrEmptyID,
		},
		{
			name:    "whitespace only id",
			q:       channel.Question{ID: "   ", Recipient: "human", Payload: "hi"},
			wantErr: channel.ErrEmptyID,
		},
		{
			name:    "empty recipient",
			q:       channel.Question{ID: "q1", Recipient: "", Payload: "hi"},
			wantErr: channel.ErrEmptyRecipient,
		},
		{
			name:    "empty payload",
			q:       channel.Question{ID: "q1", Recipient: "human", Payload: ""},
			wantErr: channel.ErrEmptyPayload,
		},
		{
			name:    "fully populated passes",
			q:       channel.Question{ID: "q1", Recipient: "human", Payload: "hi"},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.q.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
