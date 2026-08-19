package provider_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestMessageValidateName(t *testing.T) {
	cases := []struct {
		name    string
		msg     provider.Message
		wantErr error
	}{
		{
			name: "valid named user",
			msg:  provider.Message{Role: provider.RoleUser, Name: "host-frame"},
		},
		{
			name: "valid named tool with tool call id",
			msg:  provider.Message{Role: provider.RoleTool, Name: "search", ToolCallID: "call-1"},
		},
		{
			name:    "invalid named system",
			msg:     provider.Message{Role: provider.RoleSystem, Name: "host"},
			wantErr: provider.ErrNameUnexpected,
		},
		{
			name:    "invalid named assistant",
			msg:     provider.Message{Role: provider.RoleAssistant, Name: "host"},
			wantErr: provider.ErrNameUnexpected,
		},
		{
			name: "valid empty name on system",
			msg:  provider.Message{Role: provider.RoleSystem},
		},
		{
			name: "valid empty name on user",
			msg:  provider.Message{Role: provider.RoleUser},
		},
		{
			name: "valid empty name on assistant",
			msg:  provider.Message{Role: provider.RoleAssistant},
		},
		{
			name: "valid empty name on tool",
			msg:  provider.Message{Role: provider.RoleTool, ToolCallID: "call-1"},
		},
		{
			name:    "invalid unknown role with non empty name",
			msg:     provider.Message{Role: provider.Role("bogus"), Name: "host"},
			wantErr: provider.ErrUnknownRole,
		},
		{
			name:    "invalid system with name and tool call id fails name first",
			msg:     provider.Message{Role: provider.RoleSystem, Name: "host", ToolCallID: "call-1"},
			wantErr: provider.ErrNameUnexpected,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.msg.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}

func TestMessageValidateNameForm(t *testing.T) {
	cases := []struct {
		name    string
		msg     provider.Message
		wantErr error
	}{
		{
			name: "valid name at max bytes",
			msg:  provider.Message{Role: provider.RoleUser, Name: strings.Repeat("a", provider.MaxNameBytes)},
		},
		{
			name:    "invalid name over max bytes",
			msg:     provider.Message{Role: provider.RoleUser, Name: strings.Repeat("a", provider.MaxNameBytes+1)},
			wantErr: provider.ErrNameInvalid,
		},
		{
			name:    "invalid name not valid utf8",
			msg:     provider.Message{Role: provider.RoleUser, Name: string([]byte{0xff, 0xfe})},
			wantErr: provider.ErrNameInvalid,
		},
		{
			name:    "invalid name with control character",
			msg:     provider.Message{Role: provider.RoleUser, Name: "bad\x01name"},
			wantErr: provider.ErrNameInvalid,
		},
		{
			name:    "invalid named tool name over max bytes",
			msg:     provider.Message{Role: provider.RoleTool, ToolCallID: "call-1", Name: strings.Repeat("b", provider.MaxNameBytes+1)},
			wantErr: provider.ErrNameInvalid,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.msg.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}

func TestMaxNameBytes(t *testing.T) {
	if provider.MaxNameBytes != 128 {
		t.Fatalf("MaxNameBytes = %d, want 128", provider.MaxNameBytes)
	}
}

func TestSentinelsDistinctByString(t *testing.T) {
	sentinels := []error{
		provider.ErrToolCallIDUnexpected,
		provider.ErrToolCallIDRequired,
		provider.ErrUnknownRole,
		provider.ErrToolCallsUnexpected,
		provider.ErrChunkErrDoneConflict,
		provider.ErrStreamClosedEarly,
		provider.ErrNameUnexpected,
		provider.ErrNameInvalid,
		provider.ErrPromptTooLong,
	}
	seen := make(map[string]string, len(sentinels))
	for _, s := range sentinels {
		other, dup := seen[s.Error()]
		if dup {
			t.Fatalf("sentinel %q duplicates %q by string", s.Error(), other)
		}
		seen[s.Error()] = s.Error()
	}
}

func TestRunTurnValidatesIllegalNameBeforeDispatch(t *testing.T) {
	cases := []struct {
		name    string
		msg     provider.Message
		wantErr error
	}{
		{
			name:    "name on a role that may not carry one",
			msg:     provider.Message{Role: provider.RoleSystem, Name: "host"},
			wantErr: provider.ErrNameUnexpected,
		},
		{
			name:    "malformed name on a legal role",
			msg:     provider.Message{Role: provider.RoleUser, Name: "bad\x01name"},
			wantErr: provider.ErrNameInvalid,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeCompleter{name: "fake"}
			req := provider.Request{Stream: false, Messages: []provider.Message{c.msg}}

			got, err := provider.RunTurn(context.Background(), f, req)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("RunTurn() error = %v, want errors.Is %v", err, c.wantErr)
			}
			if !reflect.DeepEqual(got, provider.Response{}) {
				t.Fatalf("RunTurn() response = %+v, want zero value", got)
			}
			if f.chatCalled || f.streamCalled {
				t.Fatal("RunTurn() dispatched to the Completer despite an illegal name")
			}
		})
	}
}
