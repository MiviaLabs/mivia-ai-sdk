package dispatch_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
		name string
		opts dispatch.Options
		want error
	}{
		{
			name: "blank id",
			opts: dispatch.Options{ID: "", Room: r, Resolve: resolve},
			want: dispatch.ErrNoID,
		},
		{
			name: "whitespace id",
			opts: dispatch.Options{ID: "   ", Room: r, Resolve: resolve},
			want: dispatch.ErrNoID,
		},
		{
			name: "nil room",
			opts: dispatch.Options{ID: "endpoint-1", Room: nil, Resolve: resolve},
			want: dispatch.ErrNoRoom,
		},
		{
			name: "nil resolve",
			opts: dispatch.Options{ID: "endpoint-1", Room: r, Resolve: nil},
			want: dispatch.ErrNoResolve,
		},
		{
			name: "id checked before room",
			opts: dispatch.Options{ID: "", Room: nil, Resolve: nil},
			want: dispatch.ErrNoID,
		},
		{
			name: "room checked before resolve",
			opts: dispatch.Options{ID: "endpoint-1", Room: nil, Resolve: nil},
			want: dispatch.ErrNoRoom,
		},
		{
			name: "accept",
			opts: dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve},
			want: nil,
		},
		{
			name: "negative replay lease",
			opts: dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayLease: -time.Second},
			want: dispatch.ErrBadReplayLease,
		},
		{
			name: "sub-second replay lease",
			opts: dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayLease: 500 * time.Millisecond},
			want: dispatch.ErrBadReplayLease,
		},
		{
			name: "negative replay capacity",
			opts: dispatch.Options{ID: "endpoint-1", Room: r, Resolve: resolve, ReplayCapacity: -1},
			want: dispatch.ErrBadReplayLease,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationCase(t, tc.opts, tc.want)
		})
	}
}

// assertValidationCase runs opts through Options.Validate and New and
// checks both answer want, or both succeed when want is nil.
func assertValidationCase(t *testing.T, opts dispatch.Options, want error) {
	t.Helper()
	if err := opts.Validate(); !errors.Is(err, want) {
		t.Fatalf("Validate() error = %v, want %v", err, want)
	}
	e, err := dispatch.New(opts)
	if want == nil {
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
	if !errors.Is(err, want) {
		t.Fatalf("New() error = %v, want %v", err, want)
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
