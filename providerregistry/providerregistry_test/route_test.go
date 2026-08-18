package providerregistry_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
)

// fakeCompleter is a Completer double. It does no I/O. onChat, when
// set, runs before Chat's configured result, so a test can cancel ctx
// between order entries.
type fakeCompleter struct {
	name        string
	chatResp    provider.Response
	chatErr     error
	log         *[]string
	onChat      func()
	lastRequest provider.Request
}

func (f *fakeCompleter) Name() string { return f.name }

func (f *fakeCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	f.lastRequest = req
	if f.log != nil {
		*f.log = append(*f.log, f.name)
	}
	if f.onChat != nil {
		f.onChat()
	}
	if f.chatErr != nil {
		return provider.Response{}, f.chatErr
	}
	return f.chatResp, nil
}

func (f *fakeCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Done: true, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// newPopulatedRegistry builds a Registry with the given fakes
// registered, each under its own name, and returns it with the shared
// call log.
func newPopulatedRegistry(t *testing.T, fakes ...*fakeCompleter) (*providerregistry.Registry, *[]string) {
	t.Helper()
	r := providerregistry.New()
	log := &[]string{}
	for _, f := range fakes {
		if err := r.Register(f.name, f); err != nil {
			t.Fatalf("Register(%s) error = %v, want nil", f.name, err)
		}
		f.log = log
	}
	return r, log
}

// userRequest builds a valid non-streaming Request.
func userRequest() provider.Request {
	return provider.Request{
		Model:    "test-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
}

// TestRouteEmptyOrder covers the nil and empty-slice order forms. Both
// return ErrEmptyOrder and call no Completer.
func TestRouteEmptyOrder(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{name: "nil order", order: nil},
		{name: "empty order", order: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCompleter{name: "alpha"}
			r, log := newPopulatedRegistry(t, fake)
			resp, err := r.Route(context.Background(), userRequest(), tc.order, nil)
			if !errors.Is(err, providerregistry.ErrEmptyOrder) {
				t.Fatalf("Route() error = %v, want ErrEmptyOrder", err)
			}
			if !reflect.DeepEqual(resp, provider.Response{}) {
				t.Fatalf("Route() response = %+v, want zero value", resp)
			}
			if len(*log) != 0 {
				t.Fatalf("Route() called %v, want no calls", *log)
			}
		})
	}
}

// TestRouteUnknownName covers order entries Get cannot resolve.
func TestRouteUnknownName(t *testing.T) {
	t.Run("unknown entry first calls no completer", func(t *testing.T) {
		fake := &fakeCompleter{name: "alpha"}
		r, log := newPopulatedRegistry(t, fake)
		_, err := r.Route(context.Background(), userRequest(), []string{"missing", "alpha"}, nil)
		if !errors.Is(err, providerregistry.ErrUnknownName) {
			t.Fatalf("Route() error = %v, want ErrUnknownName", err)
		}
		if len(*log) != 0 {
			t.Fatalf("Route() called %v, want no calls before the unresolved entry", *log)
		}
	})
	t.Run("error names the missing entry", func(t *testing.T) {
		r := providerregistry.New()
		_, err := r.Route(context.Background(), userRequest(), []string{"missing"}, nil)
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("Route() error = %v, want text naming missing", err)
		}
	})
	t.Run("stops at the unresolved entry", func(t *testing.T) {
		failing := &fakeCompleter{name: "alpha", chatErr: errors.New("alpha down")}
		never := &fakeCompleter{name: "beta"}
		r, log := newPopulatedRegistry(t, failing, never)
		_, err := r.Route(context.Background(), userRequest(), []string{"alpha", "missing", "beta"}, nil)
		if !errors.Is(err, providerregistry.ErrUnknownName) {
			t.Fatalf("Route() error = %v, want ErrUnknownName", err)
		}
		want := []string{"alpha"}
		if !reflect.DeepEqual(*log, want) {
			t.Fatalf("Route() called %v, want %v and nothing past the unresolved entry", *log, want)
		}
	})
}

// TestRouteSingleNameSucceeds covers the one-name order whose
// Completer succeeds: the Response returns unchanged.
func TestRouteSingleNameSucceeds(t *testing.T) {
	want := provider.Response{
		Model:        "test-model",
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "hi"},
		FinishReason: "stop",
	}
	fake := &fakeCompleter{name: "alpha", chatResp: want}
	r, log := newPopulatedRegistry(t, fake)

	got, err := r.Route(context.Background(), userRequest(), []string{"alpha"}, nil)
	if err != nil {
		t.Fatalf("Route() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Route() response = %+v, want %+v unchanged", got, want)
	}
	if !reflect.DeepEqual(*log, []string{"alpha"}) {
		t.Fatalf("Route() called %v, want [alpha]", *log)
	}
}

// TestRouteFallsThroughOnRetryable covers the two-name order whose
// first Completer fails with a retryable error: Route returns the
// second Completer's Response, and both fakes ran, in order.
func TestRouteFallsThroughOnRetryable(t *testing.T) {
	first := &fakeCompleter{name: "alpha", chatErr: errors.New("alpha down")}
	want := provider.Response{
		Model:        "test-model",
		Message:      provider.Message{Role: provider.RoleAssistant, Content: "from beta"},
		FinishReason: "stop",
	}
	second := &fakeCompleter{name: "beta", chatResp: want}
	r, log := newPopulatedRegistry(t, first, second)

	got, err := r.Route(context.Background(), userRequest(), []string{"alpha", "beta"}, func(error) bool { return true })
	if err != nil {
		t.Fatalf("Route() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Route() response = %+v, want beta's %+v", got, want)
	}
	if wantLog := []string{"alpha", "beta"}; !reflect.DeepEqual(*log, wantLog) {
		t.Fatalf("Route() called %v, want %v in order", *log, wantLog)
	}
}

// TestRouteNilRetryableFallsThrough covers the nil predicate: it falls
// through on every error, same as a predicate that always returns
// true.
func TestRouteNilRetryableFallsThrough(t *testing.T) {
	first := &fakeCompleter{name: "alpha", chatErr: errors.New("alpha down")}
	want := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "from beta"}}
	second := &fakeCompleter{name: "beta", chatResp: want}
	r, _ := newPopulatedRegistry(t, first, second)

	got, err := r.Route(context.Background(), userRequest(), []string{"alpha", "beta"}, nil)
	if err != nil {
		t.Fatalf("Route() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Route() response = %+v, want beta's %+v", got, want)
	}
}

// TestRouteStopsOnNonRetryable covers the two-name order whose first
// Completer fails with a non-retryable error: Route returns that error
// unwrapped and never calls the second fake.
func TestRouteStopsOnNonRetryable(t *testing.T) {
	fatal := errors.New("fatal alpha failure")
	first := &fakeCompleter{name: "alpha", chatErr: fatal}
	second := &fakeCompleter{name: "beta"}
	r, log := newPopulatedRegistry(t, first, second)

	got, err := r.Route(context.Background(), userRequest(), []string{"alpha", "beta"}, func(error) bool { return false })
	if err != fatal {
		t.Fatalf("Route() error = %v, want the first fake's error %v unwrapped (identity)", err, fatal)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("Route() response = %+v, want zero value", got)
	}
	if wantLog := []string{"alpha"}; !reflect.DeepEqual(*log, wantLog) {
		t.Fatalf("Route() called %v, want %v: the second fake must not run", *log, wantLog)
	}
}

// TestRouteAllFailed covers orders where every name fails and every
// failure is retryable: Route returns ErrAllFailed, and errors.Unwrap
// on that error yields the last fake's error.
func TestRouteAllFailed(t *testing.T) {
	firstErr := errors.New("alpha down")
	secondErr := errors.New("beta down")
	for _, tc := range []struct {
		name     string
		fakes    []*fakeCompleter
		order    []string
		wantLast error
	}{
		{
			name:     "one name",
			fakes:    []*fakeCompleter{{name: "alpha", chatErr: firstErr}},
			order:    []string{"alpha"},
			wantLast: firstErr,
		},
		{
			name:     "two names",
			fakes:    []*fakeCompleter{{name: "alpha", chatErr: firstErr}, {name: "beta", chatErr: secondErr}},
			order:    []string{"alpha", "beta"},
			wantLast: secondErr,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newPopulatedRegistry(t, tc.fakes...)
			got, err := r.Route(context.Background(), userRequest(), tc.order, func(error) bool { return true })
			if !errors.Is(err, providerregistry.ErrAllFailed) {
				t.Fatalf("Route() error = %v, want errors.Is ErrAllFailed", err)
			}
			if text := err.Error(); !strings.Contains(text, providerregistry.ErrAllFailed.Error()) || !strings.Contains(text, tc.wantLast.Error()) {
				t.Fatalf("Route() error text = %q, want both ErrAllFailed and the last error named", text)
			}
			if unwrapped := errors.Unwrap(err); unwrapped != tc.wantLast {
				t.Fatalf("errors.Unwrap() = %v, want the last fake's error %v", unwrapped, tc.wantLast)
			}
			if !errors.Is(err, tc.wantLast) {
				t.Fatalf("Route() error = %v, want errors.Is the last fake's error", err)
			}
			if !reflect.DeepEqual(got, provider.Response{}) {
				t.Fatalf("Route() response = %+v, want zero value", got)
			}
		})
	}
}

// TestRouteContextCanceledBetweenEntries covers the canceled-ctx stop:
// a test-controlled fake cancels ctx during its Chat, Route checks
// ctx.Err() before the next attempt, returns ctx.Err(), and never
// calls the remaining names.
func TestRouteContextCanceledBetweenEntries(t *testing.T) {
	cancelErr := errors.New("alpha down")
	first := &fakeCompleter{name: "alpha", chatErr: cancelErr}
	second := &fakeCompleter{name: "beta"}
	third := &fakeCompleter{name: "gamma"}
	r, log := newPopulatedRegistry(t, first, second, third)

	ctx, cancel := context.WithCancel(context.Background())
	first.onChat = cancel

	got, err := r.Route(ctx, userRequest(), []string{"alpha", "beta", "gamma"}, func(error) bool { return true })
	if want := ctx.Err(); err != want {
		t.Fatalf("Route() error = %v, want the unwrapped ctx.Err() %v (identity)", err, want)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Route() error = %v, want errors.Is context.Canceled", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("Route() response = %+v, want zero value", got)
	}
	if wantLog := []string{"alpha"}; !reflect.DeepEqual(*log, wantLog) {
		t.Fatalf("Route() called %v, want %v: the remaining names must not run", *log, wantLog)
	}
}
