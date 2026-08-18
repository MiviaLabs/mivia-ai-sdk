// Package channel gives every part of this SDK that must ask a
// question and wait for a typed answer one shared shape to build a
// closure from: Question, Answer, and Notifier. channel supplies the
// shape only. It sends no bytes over any real transport.
package channel

import (
	"context"
	"errors"
	"strings"
)

// Sentinel errors returned by Question.Validate and Answer.Validate.
// Test with errors.Is.
var (
	ErrEmptyID         = errors.New("channel: question id must not be empty")
	ErrEmptyRecipient  = errors.New("channel: recipient must not be empty")
	ErrEmptyPayload    = errors.New("channel: payload must not be empty")
	ErrEmptyQuestionID = errors.New("channel: answer question id must not be empty")
)

// Question is one thing being asked. ID names the question so an
// Answer can reference it; the caller sets ID, channel never
// generates one. Recipient names who or what should answer: a human
// handle, an agent identity, or any string the caller's Notifier
// interprets. Payload carries the question's content as an opaque
// string; a caller that wants a signed envelope.Message here encodes
// it first and decodes it inside its own Notifier.
type Question struct {
	ID        string
	Recipient string
	Payload   string
}

// Answer is one response. QuestionID echoes the Question.ID it
// answers. Approved gives a yes-or-no reading for the common case.
// Payload carries free-form response content, for example a query's
// actual answer text; a caller that needs only yes-or-no leaves it
// empty.
type Answer struct {
	QuestionID string
	Approved   bool
	Payload    string
}

// Validate rejects an empty ID, an empty Recipient, and an empty
// Payload, each with its own sentinel error. A whitespace-only
// string counts as empty: Validate trims with strings.TrimSpace
// before comparing.
func (q Question) Validate() error {
	if strings.TrimSpace(q.ID) == "" {
		return ErrEmptyID
	}
	if strings.TrimSpace(q.Recipient) == "" {
		return ErrEmptyRecipient
	}
	if strings.TrimSpace(q.Payload) == "" {
		return ErrEmptyPayload
	}
	return nil
}

// Validate rejects an empty QuestionID, after the same
// strings.TrimSpace trim. It rejects nothing else; a decline
// (Approved: false) needs no Payload, and Approved is a plain bool
// with no invalid state.
func (a Answer) Validate() error {
	if strings.TrimSpace(a.QuestionID) == "" {
		return ErrEmptyQuestionID
	}
	return nil
}

// Notifier is a caller-implemented channel: prompt a human, call
// Slack, call another agent, or any transport the caller owns.
// channel ships no implementation. Notifier is a func type with no
// method, so a caller assigns any matching closure with no wrapper
// type.
type Notifier func(ctx context.Context, q Question) (Answer, error)
