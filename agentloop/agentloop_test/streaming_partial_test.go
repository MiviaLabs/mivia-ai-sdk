package agentloop_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	sdkagentloop "github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

type streamingCompleter struct {
	mu        sync.Mutex
	wroteTwo  chan struct{}
	cancelled chan struct{}
}

func (c *streamingCompleter) Name() string { return "streaming-completer" }

func (c *streamingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if req.StreamingWriter != nil {
		_, _ = req.StreamingWriter.Write([]byte("alpha "))
		_, _ = req.StreamingWriter.Write([]byte("beta "))
		// Signal after the two writes and before blocking: the
		// trigger goroutine waits on wroteTwo, and Chat waits on
		// the steer, so signalling after the select deadlocks.
		c.mu.Lock()
		if c.wroteTwo != nil {
			select {
			case c.wroteTwo <- struct{}{}:
			default:
			}
		}
		c.mu.Unlock()
		select {
		case <-c.cancelled:
		case <-ctx.Done():
		}
		return provider.Response{}, ctx.Err()
	}
	return provider.Response{Message: provider.Message{Content: "no writer"}, FinishReason: "stop"}, nil
}

func (c *streamingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	go func() {
		defer close(ch)
		_, _ = req.StreamingWriter.Write([]byte("delta-1 "))
		ch <- provider.Chunk{Delta: "delta-1 "}
		_, _ = req.StreamingWriter.Write([]byte("delta-2 "))
		ch <- provider.Chunk{Delta: "delta-2 "}
		_, _ = req.StreamingWriter.Write([]byte("delta-3 "))
		ch <- provider.Chunk{Delta: "delta-3 "}
		ch <- provider.Chunk{Done: true, FinishReason: "stop"}
	}()
	return ch, nil
}

func TestRunResultFinalCapturesStreamedPartialOnCancel(t *testing.T) {
	comp := &streamingCompleter{
		wroteTwo:  make(chan struct{}, 1),
		cancelled: make(chan struct{}),
	}
	loop, err := sdkagentloop.New(sdkagentloop.Options{
		Completer:       comp,
		Tools:           tools.New(),
		Model:           "m",
		StreamingWriter: &strings.Builder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	steer := sdkagentloop.NewSteer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-comp.wroteTwo
		steer.Trigger()
	}()
	res, _ := loop.RunSteerable(ctx, []provider.Message{{Role: provider.RoleUser, Content: "x"}}, steer)
	if res.Stop != sdkagentloop.StopSteered {
		t.Fatalf("Stop = %v, want Steered (cancel captured partial)", res.Stop)
	}
	got := res.Final.Content
	for _, want := range []string{"alpha ", "beta "} {
		if !strings.Contains(got, want) {
			t.Fatalf("Final.Content missing %q: %q", want, got)
		}
	}
}
