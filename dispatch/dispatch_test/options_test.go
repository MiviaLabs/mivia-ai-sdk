package dispatch_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestNewValidation runs every New sentinel and the accept path.
func TestNewValidation(t *testing.T) {
	founder, _ := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	resolve := resolveAlways(echoHandler{})

	cases := []struct {
		name       string
		opts       dispatch.Options
		wantField  string
		wantAccept bool
	}{
		{
			name:      "blank id",
			opts:      dispatch.Options{ID: "", Room: r, Resolve: resolve},
			wantField: "ID",
		},
		{
			name:      "whitespace id",
			opts:      dispatch.Options{ID: "   ", Room: r, Resolve: resolve},
			wantField: "ID",
		},
		{
			name:      "nil room",
			opts:      dispatch.Options{ID: "endpoint-1", Room: nil, Resolve: resolve},
			wantField: "Room",
		},
		{
			name:      "nil resolve",
			opts:      dispatch.Options{ID: "endpoint-1", Room: r, Resolve: nil},
			wantField: "Resolve",
		},
		{
			name:      "id checked before room",
			opts:      dispatch.Options{ID: "", Room: nil, Resolve: nil},
			wantField: "ID",
		},
		{
			name:      "room checked before resolve",
			opts:      dispatch.Options{ID: "endpoint-1", Room: nil, Resolve: nil},
			wantField: "Room",
		},
		{
			name:       "accept",
			opts:       dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve},
			wantAccept: true,
		},
		{
			name:      "negative replay lease",
			opts:      dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayLease: -time.Second},
			wantField: "ReplayLease",
		},
		{
			name:      "sub-second replay lease",
			opts:      dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayLease: 500 * time.Millisecond},
			wantField: "ReplayLease",
		},
		{
			name:       "replay lease exactly one second is valid",
			opts:       dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayLease: time.Second},
			wantAccept: true,
		},
		{
			name:      "negative replay capacity",
			opts:      dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayCapacity: -1},
			wantField: "ReplayCapacity",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationCase(t, tc.opts, tc.wantField, tc.wantAccept)
		})
	}
}

// assertValidationCase runs opts through Options.Validate and New. A
// wantAccept case checks both succeed; otherwise it checks both fail
// with dispatch.ErrInvalidOptions, naming wantField in the message.
func assertValidationCase(t *testing.T, opts dispatch.Options, wantField string, wantAccept bool) {
	t.Helper()
	err := opts.Validate()
	if wantAccept {
		if err != nil {
			t.Fatalf("Validate() error = %v, want nil", err)
		}
	} else if !errors.Is(err, dispatch.ErrInvalidOptions) || !strings.Contains(err.Error(), wantField) {
		t.Fatalf("Validate() error = %v, want ErrInvalidOptions naming %q", err, wantField)
	}
	e, err := dispatch.New(opts)
	if wantAccept {
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}
		if e == nil {
			t.Fatal("New() endpoint is nil")
		}
		if e.Handler() == nil {
			t.Fatal("Handler() is nil")
		}
		return
	}
	if !errors.Is(err, dispatch.ErrInvalidOptions) || !strings.Contains(err.Error(), wantField) {
		t.Fatalf("New() error = %v, want ErrInvalidOptions naming %q", err, wantField)
	}
	if e != nil {
		t.Fatalf("New() endpoint = %v, want nil", e)
	}
}

// TestNewBuildsBusWhenNil proves New builds and subscribes a bus when
// Options.Bus is nil: a full request through the resulting Endpoint
// still answers a confirmed ack, so EmitMessageDelivered and
// EmitMessageAcked never hit an unsubscribed-name error.
func TestNewBuildsBusWhenNil(t *testing.T) {
	founder, key := newMember(t)
	r := newRoom(t, "room-1", founder, "")
	e, err := dispatch.New(dispatch.Options{
		ID:      "endpoint-1",
		Room:    r,
		Resolve: resolveAlways(echoHandler{prefix: "got: "}),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv := httptest.NewServer(e.Handler())
	defer srv.Close()

	probeMsg := signIn(t, key, "room-1", "m-1-probe", "hello")
	data, err := probeMsg.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	resp, err := http.Post(srv.URL, "application/x-ndjson", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := signIn(t, key, "room-1", "m-1", "hello")
	results, err := dispatch.Send(context.Background(), srv.URL, []envelope.Message{msg})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("results[0].Err = %v, want nil", results[0].Err)
	}
	if results[0].Ack.Status != envelope.AckConfirmed {
		t.Fatalf("ack status = %q, want %q", results[0].Ack.Status, envelope.AckConfirmed)
	}
}
