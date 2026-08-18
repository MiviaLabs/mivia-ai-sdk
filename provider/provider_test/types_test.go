package provider_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestRequestZeroValue(t *testing.T) {
	var req provider.Request
	if req.Model != "" {
		t.Fatalf("zero Request.Model = %q, want empty", req.Model)
	}
	if req.Stream {
		t.Fatal("zero Request.Stream = true, want false")
	}
	if len(req.Messages) != 0 {
		t.Fatalf("zero Request.Messages len = %d, want 0", len(req.Messages))
	}
}

func TestResponseZeroValue(t *testing.T) {
	var resp provider.Response
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("zero Response.ToolCalls len = %d, want 0", len(resp.ToolCalls))
	}
	if resp.Model != "" {
		t.Fatalf("zero Response.Model = %q, want empty", resp.Model)
	}
}

func TestRoleConstants(t *testing.T) {
	cases := []struct {
		role provider.Role
		want string
	}{
		{provider.RoleSystem, "system"},
		{provider.RoleUser, "user"},
		{provider.RoleAssistant, "assistant"},
		{provider.RoleTool, "tool"},
	}
	for _, c := range cases {
		if string(c.role) != c.want {
			t.Errorf("Role %v as string = %q, want %q", c.role, string(c.role), c.want)
		}
	}
}

func TestMessageValidate(t *testing.T) {
	cases := []struct {
		name    string
		msg     provider.Message
		wantErr error
	}{
		{
			name: "valid user empty tool call id",
			msg:  provider.Message{Role: provider.RoleUser},
		},
		{
			name: "valid tool with tool call id",
			msg:  provider.Message{Role: provider.RoleTool, ToolCallID: "call-1"},
		},
		{
			name: "valid system empty tool call id",
			msg:  provider.Message{Role: provider.RoleSystem},
		},
		{
			name: "valid assistant empty tool call id",
			msg:  provider.Message{Role: provider.RoleAssistant},
		},
		{
			name:    "invalid user with tool call id",
			msg:     provider.Message{Role: provider.RoleUser, ToolCallID: "call-1"},
			wantErr: provider.ErrToolCallIDUnexpected,
		},
		{
			name:    "invalid system with tool call id",
			msg:     provider.Message{Role: provider.RoleSystem, ToolCallID: "call-1"},
			wantErr: provider.ErrToolCallIDUnexpected,
		},
		{
			name:    "invalid assistant with tool call id",
			msg:     provider.Message{Role: provider.RoleAssistant, ToolCallID: "call-1"},
			wantErr: provider.ErrToolCallIDUnexpected,
		},
		{
			name:    "invalid tool with empty tool call id",
			msg:     provider.Message{Role: provider.RoleTool},
			wantErr: provider.ErrToolCallIDRequired,
		},
		{
			name:    "invalid unknown role",
			msg:     provider.Message{Role: provider.Role("bogus")},
			wantErr: provider.ErrUnknownRole,
		},
		{
			name:    "invalid unknown role with tool call id",
			msg:     provider.Message{Role: provider.Role("bogus"), ToolCallID: "call-1"},
			wantErr: provider.ErrUnknownRole,
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

func TestChunkValidate(t *testing.T) {
	cases := []struct {
		name    string
		chunk   provider.Chunk
		wantErr error
	}{
		{
			name:  "valid no err not done",
			chunk: provider.Chunk{},
		},
		{
			name:  "valid no err done",
			chunk: provider.Chunk{Done: true},
		},
		{
			name:  "valid err not done",
			chunk: provider.Chunk{Err: errors.New("boom")},
		},
		{
			name:    "invalid err and done",
			chunk:   provider.Chunk{Err: errors.New("boom"), Done: true},
			wantErr: provider.ErrChunkErrDoneConflict,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.chunk.Validate()
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
