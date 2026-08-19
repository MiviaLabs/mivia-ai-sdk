// Package spool_test exercises spool.Spool and spool.SpoolTool.
package spool_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// fakeStore is a spool.ContentStore backed by an in-memory map, with
// optional per-ref hooks for injecting failures.
type fakeStore struct {
	mu       sync.Mutex
	blobs    map[string][]byte
	getFail  map[string]error
	putFail  error
	putCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{blobs: make(map[string][]byte), getFail: make(map[string]error)}
}

func refFor(content []byte) string {
	sum := sha256.Sum256(content)
	return "ref-" + hex.EncodeToString(sum[:8])
}

func (f *fakeStore) Put(content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls++
	if f.putFail != nil {
		return "", f.putFail
	}
	ref := refFor(content)
	cp := make([]byte, len(content))
	copy(cp, content)
	f.blobs[ref] = cp
	return ref, nil
}

func (f *fakeStore) Get(ref string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.getFail[ref]; ok {
		return nil, err
	}
	b, ok := f.blobs[ref]
	if !ok {
		return nil, fmt.Errorf("fakeStore: no blob for %s", ref)
	}
	return b, nil
}

func TestSpoolLoadRoundTrip(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 4096)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	data := []byte("hello world")
	view, ref, err := sp.Spool(ctx, "alice", data)
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	if view != string(data) {
		t.Errorf("view = %q, want unchanged for small data", view)
	}
	got, err := sp.Load(ctx, "alice", ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Load = %q, want %q", got, data)
	}
}

func TestSpoolOversizedTruncatesView(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), 10000)
	view, ref, err := sp.Spool(ctx, "alice", data)
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	if len(view) >= len(data) {
		t.Errorf("view len = %d, want shorter than data len %d", len(view), len(data))
	}
	got, err := sp.Load(ctx, "alice", ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Load returned %d bytes, want the full %d", len(got), len(data))
	}
}

func TestLoadWrongPrincipal(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 4096)
	ctx := context.Background()
	_, ref, err := sp.Spool(ctx, "alice", []byte("secret"))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	_, err = sp.Load(ctx, "bob", ref)
	if !errors.Is(err, spool.ErrWrongPrincipal) {
		t.Errorf("Load err = %v, want ErrWrongPrincipal", err)
	}
}

func TestLoadUnknownRef(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 4096)
	_, err := sp.Load(context.Background(), "alice", "no-such-ref")
	if !errors.Is(err, spool.ErrUnknownRef) {
		t.Errorf("Load err = %v, want ErrUnknownRef", err)
	}
}

func TestLoadStoreGetFailureWrapsUnknownRef(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 4096)
	ctx := context.Background()
	_, ref, err := sp.Spool(ctx, "alice", []byte("data"))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	store.mu.Lock()
	store.getFail[ref] = errors.New("evicted independently")
	store.mu.Unlock()

	_, err = sp.Load(ctx, "alice", ref)
	if !errors.Is(err, spool.ErrUnknownRef) {
		t.Errorf("Load err = %v, want ErrUnknownRef", err)
	}
}

func TestNewSpoolNonPositiveBudget(t *testing.T) {
	tests := []struct {
		name          string
		maxGrantBytes int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spool.NewSpool(newFakeStore(), tt.maxGrantBytes)
			if !errors.Is(err, spool.ErrNoBudget) {
				t.Errorf("NewSpool(%d) err = %v, want ErrNoBudget", tt.maxGrantBytes, err)
			}
		})
	}
}

func TestGrantExpiryEvictsOldest(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 10)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	_, oldRef, err := sp.Spool(ctx, "alice", []byte("aaaaaaaaaa"))
	if err != nil {
		t.Fatalf("Spool old: %v", err)
	}
	_, newRef, err := sp.Spool(ctx, "alice", []byte("bbbbbbbbbb"))
	if err != nil {
		t.Fatalf("Spool new: %v", err)
	}

	if _, err := sp.Load(ctx, "alice", oldRef); !errors.Is(err, spool.ErrUnknownRef) {
		t.Errorf("Load oldRef err = %v, want ErrUnknownRef (evicted)", err)
	}
	if _, err := sp.Load(ctx, "alice", newRef); err != nil {
		t.Errorf("Load newRef err = %v, want nil (still live)", err)
	}
}

func TestSpoolReSpoolSameContentRefreshesOrder(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 10)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	data := []byte("aaaaaaaaaa")
	_, ref1, err := sp.Spool(ctx, "alice", data)
	if err != nil {
		t.Fatalf("Spool 1: %v", err)
	}
	_, ref2, err := sp.Spool(ctx, "alice", data)
	if err != nil {
		t.Fatalf("Spool 2: %v", err)
	}
	if ref1 != ref2 {
		t.Fatalf("ref1 = %q, ref2 = %q, want identical refs for identical content", ref1, ref2)
	}
	if _, err := sp.Load(ctx, "alice", ref1); err != nil {
		t.Errorf("Load re-spooled ref: %v, want nil (still live)", err)
	}
}

func TestPrincipalRoundTrip(t *testing.T) {
	ctx := spool.WithPrincipal(context.Background(), "alice")
	p, ok := spool.PrincipalFrom(ctx)
	if !ok || p != "alice" {
		t.Errorf("PrincipalFrom = %q,%v, want alice,true", p, ok)
	}
	_, ok = spool.PrincipalFrom(context.Background())
	if ok {
		t.Errorf("PrincipalFrom on bare context = true, want false")
	}
}

// stringTool returns a fixed string result, or errFail if set.
type stringTool struct {
	name    string
	result  string
	errFail error
}

func (s stringTool) Name() string { return s.name }

func (s stringTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if s.errFail != nil {
		return tools.Out{}, s.errFail
	}
	return tools.Out{Value: s.result}, nil
}

func TestSpoolToolPassesThroughSmallResult(t *testing.T) {
	store := newFakeStore()
	wrapped := spool.SpoolTool("wrapper-name", 100, store, stringTool{name: "inner", result: "short"})
	if wrapped.Name() != "wrapper-name" {
		t.Errorf("Name() = %q, want the wrapper's own name", wrapped.Name())
	}
	ctx := context.Background()
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "short" {
		t.Errorf("Out.Value = %v, want unchanged inner result", out.Value)
	}
}

func TestSpoolToolTruncatesLargeResult(t *testing.T) {
	store := newFakeStore()
	full := strings.Repeat("y", 1000)
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: full})
	ctx := spool.WithPrincipal(context.Background(), "alice")
	out, err := wrapped.Run(ctx, tools.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	view, ok := out.Value.(string)
	if !ok || len(view) >= len(full) {
		t.Fatalf("Out.Value = %v, want a truncated view", out.Value)
	}
	ref := refFor([]byte(full))
	if !strings.Contains(view, ref) {
		t.Errorf("view %q does not name ref %q", view, ref)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != full {
		t.Errorf("store.Get(ref) = %q,%v, want the full inner result", got, err)
	}
}

func TestSpoolToolNoPrincipal(t *testing.T) {
	store := newFakeStore()
	small := spool.SpoolTool("t", 100, store, stringTool{name: "inner", result: "short"})
	large := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: strings.Repeat("z", 1000)})
	ctx := context.Background()

	if _, err := small.Run(ctx, tools.InOut{}); err != nil {
		t.Errorf("small.Run with no principal err = %v, want nil (no grant needed)", err)
	}
	_, err := large.Run(ctx, tools.InOut{})
	if !errors.Is(err, spool.ErrNoPrincipal) {
		t.Errorf("large.Run with no principal err = %v, want ErrNoPrincipal", err)
	}
}

func TestSpoolToolInnerErrorPassesThrough(t *testing.T) {
	store := newFakeStore()
	innerErr := errors.New("inner blew up")
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", errFail: innerErr})
	_, err := wrapped.Run(context.Background(), tools.InOut{})
	if !errors.Is(err, innerErr) {
		t.Errorf("Run err = %v, want inner error passed through", err)
	}
	if store.putCalls != 0 {
		t.Errorf("store.putCalls = %d, want 0 (no spooling on error)", store.putCalls)
	}
}

func TestSpoolToolStorePutFailure(t *testing.T) {
	store := newFakeStore()
	store.putFail = errors.New("store is full")
	wrapped := spool.SpoolTool("t", 10, store, stringTool{name: "inner", result: strings.Repeat("q", 1000)})
	ctx := spool.WithPrincipal(context.Background(), "alice")
	_, err := wrapped.Run(ctx, tools.InOut{})
	if !errors.Is(err, store.putFail) {
		t.Errorf("Run err = %v, want the store's Put failure", err)
	}
}

// profiledTool implements tools.ProfiledTool, tools.ResultBudgetTool,
// and tools.PrivilegedTool with fixed values.
type profiledTool struct {
	stringTool
}

func (p profiledTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}

func (p profiledTool) MaxResultBytes() int { return 42 }

func (p profiledTool) Privileged() bool { return true }

func TestSpoolToolForwardsOptionalInterfaces(t *testing.T) {
	store := newFakeStore()
	inner := profiledTool{stringTool: stringTool{name: "inner", result: "x"}}
	wrapped := spool.SpoolTool("t", 100, store, inner)

	wantProfile := tools.ExecutionProfileOf(inner)
	gotProfile := tools.ExecutionProfileOf(wrapped)
	if gotProfile != wantProfile {
		t.Errorf("ExecutionProfileOf(wrapper) = %+v, want %+v", gotProfile, wantProfile)
	}

	wantBudget, wantOK := tools.ResultBudgetOf(inner)
	gotBudget, gotOK := tools.ResultBudgetOf(wrapped)
	if gotBudget != wantBudget || gotOK != wantOK {
		t.Errorf("ResultBudgetOf(wrapper) = %d,%v, want %d,%v", gotBudget, gotOK, wantBudget, wantOK)
	}

	if tools.IsPrivileged(wrapped) != tools.IsPrivileged(inner) {
		t.Errorf("IsPrivileged(wrapper) = %v, want %v", tools.IsPrivileged(wrapped), tools.IsPrivileged(inner))
	}
}

func TestSpoolToolNoOptionalInterfacesReportsUnimplemented(t *testing.T) {
	store := newFakeStore()
	inner := stringTool{name: "inner", result: "x"}
	wrapped := spool.SpoolTool("t", 100, store, inner)

	gotBudget, gotOK := tools.ResultBudgetOf(wrapped)
	if gotOK {
		t.Errorf("ResultBudgetOf(wrapper over non-implementing inner) = %d,%v, want _,false", gotBudget, gotOK)
	}
	if gotBudget != 0 {
		t.Errorf("ResultBudgetOf(wrapper) budget = %d, want 0", gotBudget)
	}

	gotProfile := tools.ExecutionProfileOf(wrapped)
	if gotProfile != (tools.ExecutionProfile{}) {
		t.Errorf("ExecutionProfileOf(wrapper) = %+v, want zero value", gotProfile)
	}

	if tools.IsPrivileged(wrapped) {
		t.Errorf("IsPrivileged(wrapper) = true, want false")
	}
}

func TestSpoolLoadConcurrent(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 200)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			principal := fmt.Sprintf("p%d", i)
			data := []byte(fmt.Sprintf("payload-%d", i))
			_, ref, err := sp.Spool(ctx, principal, data)
			if err != nil {
				t.Errorf("goroutine %d Spool: %v", i, err)
				return
			}
			got, err := sp.Load(ctx, principal, ref)
			if err != nil {
				// Eviction under a tight budget with concurrent writers
				// is expected; only ErrUnknownRef is a valid outcome.
				if !errors.Is(err, spool.ErrUnknownRef) {
					t.Errorf("goroutine %d Load: %v, want nil or ErrUnknownRef", i, err)
				}
				return
			}
			if !bytes.Equal(got, data) {
				t.Errorf("goroutine %d Load = %q, want %q", i, got, data)
			}
		}(i)
	}
	wg.Wait()
}
