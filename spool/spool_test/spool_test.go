// Package spool_test exercises spool.Spool and spool.SpoolTool.
package spool_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
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

func TestSpoolReSpoolSameContentMovesToBackOfOrder(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 20)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	ctx := context.Background()
	first := []byte("aaaaaaaaaa")
	second := []byte("bbbbbbbbbb")
	_, ref1, err := sp.Spool(ctx, "alice", first)
	if err != nil {
		t.Fatalf("Spool first: %v", err)
	}
	if _, _, err := sp.Spool(ctx, "alice", second); err != nil {
		t.Fatalf("Spool second: %v", err)
	}
	// Budget holds both (10+10=20). Re-spooling first's identical
	// content must move it to the back of the eviction order; a third
	// grant that would only evict the true oldest should then evict
	// second, not first.
	if _, _, err := sp.Spool(ctx, "alice", first); err != nil {
		t.Fatalf("Spool re-spool first: %v", err)
	}
	third := []byte("cccccccccc")
	if _, _, err := sp.Spool(ctx, "alice", third); err != nil {
		t.Fatalf("Spool third: %v", err)
	}

	if _, err := sp.Load(ctx, "alice", ref1); err != nil {
		t.Errorf("Load ref1 (re-spooled, should be freshest) err = %v, want nil", err)
	}
	secondRef := refFor(second)
	if _, err := sp.Load(ctx, "alice", secondRef); !errors.Is(err, spool.ErrUnknownRef) {
		t.Errorf("Load secondRef err = %v, want ErrUnknownRef (true oldest, evicted)", err)
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
