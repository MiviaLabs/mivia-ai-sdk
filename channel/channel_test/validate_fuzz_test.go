package channel_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
)

// FuzzQuestionValidate feeds arbitrary ID, Recipient, and Payload
// strings to Question.Validate. It must never panic, and the result
// must match the documented rule: each field is checked in order
// (ID, Recipient, Payload) after strings.TrimSpace, first empty wins.
func FuzzQuestionValidate(f *testing.F) {
	seeds := []string{"", " ", "\t", "\n", "q1", "  q1  ", "human", "hi"}
	for _, id := range seeds {
		for _, recipient := range seeds {
			f.Add(id, recipient, "hi")
		}
	}
	f.Fuzz(func(t *testing.T, id, recipient, payload string) {
		q := channel.Question{ID: id, Recipient: recipient, Payload: payload}
		err := q.Validate()

		switch {
		case strings.TrimSpace(id) == "":
			if !errors.Is(err, channel.ErrInvalidOptions) || !strings.Contains(err.Error(), "ID") {
				t.Fatalf("Validate() with empty id %q = %v, want errors.Is ErrInvalidOptions naming ID", id, err)
			}
		case strings.TrimSpace(recipient) == "":
			if !errors.Is(err, channel.ErrInvalidOptions) || !strings.Contains(err.Error(), "Recipient") {
				t.Fatalf("Validate() with empty recipient %q = %v, want errors.Is ErrInvalidOptions naming Recipient", recipient, err)
			}
		case strings.TrimSpace(payload) == "":
			if !errors.Is(err, channel.ErrInvalidOptions) || !strings.Contains(err.Error(), "Payload") {
				t.Fatalf("Validate() with empty payload %q = %v, want errors.Is ErrInvalidOptions naming Payload", payload, err)
			}
		default:
			if err != nil {
				t.Fatalf("Validate() with id %q recipient %q payload %q = %v, want nil", id, recipient, payload, err)
			}
		}
	})
}

// FuzzAnswerValidate feeds arbitrary QuestionID strings to
// Answer.Validate. It must never panic, and must return
// ErrInvalidOptions naming QuestionID exactly when
// strings.TrimSpace(QuestionID) is empty, nil otherwise; Approved and
// Payload never affect the result.
func FuzzAnswerValidate(f *testing.F) {
	seeds := []string{"", " ", "\t", "\n", "q1", "  q1  "}
	for _, id := range seeds {
		f.Add(id, true)
		f.Add(id, false)
	}
	f.Fuzz(func(t *testing.T, questionID string, approved bool) {
		a := channel.Answer{QuestionID: questionID, Approved: approved, Payload: "p"}
		err := a.Validate()

		if strings.TrimSpace(questionID) == "" {
			if !errors.Is(err, channel.ErrInvalidOptions) || !strings.Contains(err.Error(), "QuestionID") {
				t.Fatalf("Validate() with empty question id %q = %v, want errors.Is ErrInvalidOptions naming QuestionID", questionID, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Validate() with question id %q = %v, want nil", questionID, err)
		}
	})
}
