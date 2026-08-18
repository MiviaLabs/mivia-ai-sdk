package channel_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// TestAnswerValidate covers red-green cases for Answer.Validate: an
// empty QuestionID, a whitespace-only QuestionID (proving the trim
// rule), Approved true with empty Payload, Approved false with empty
// Payload, and a fully populated value.
func TestAnswerValidate(t *testing.T) {
	cases := []struct {
		name    string
		a       channel.Answer
		wantErr error
	}{
		{
			name:    "empty question id",
			a:       channel.Answer{QuestionID: "", Approved: true, Payload: "ok"},
			wantErr: channel.ErrEmptyQuestionID,
		},
		{
			name:    "whitespace only question id",
			a:       channel.Answer{QuestionID: "   ", Approved: true, Payload: "ok"},
			wantErr: channel.ErrEmptyQuestionID,
		},
		{
			name:    "approved true empty payload passes",
			a:       channel.Answer{QuestionID: "q1", Approved: true, Payload: ""},
			wantErr: nil,
		},
		{
			name:    "approved false empty payload passes",
			a:       channel.Answer{QuestionID: "q1", Approved: false, Payload: ""},
			wantErr: nil,
		},
		{
			name:    "fully populated passes",
			a:       channel.Answer{QuestionID: "q1", Approved: true, Payload: "ok"},
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.a.Validate()
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
