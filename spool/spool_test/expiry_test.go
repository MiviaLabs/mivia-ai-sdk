// Expiry tests for Spool grants: SpoolExpiring, Expire, GrantExpiry,
// and the lazy ErrExpired drop on Load.
package spool_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/spool"
)

// awaitPast spins until the wall clock passes t. Expiry reads
// time.Now inside Spool, so a test must let the clock move past the
// stored moment before Load; this replaces time.Sleep with a wait
// that cannot end early.
func awaitPast(t time.Time) {
	for time.Now().Before(t) {
		runtime.Gosched()
	}
}

func TestSpoolExpiringRoundTripBeforeExpiry(t *testing.T) {
	store := newFakeStore()
	sp, err := spool.NewSpool(store, 1024)
	if err != nil {
		t.Fatalf("NewSpool: %v", err)
	}
	_, ref, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), time.Minute)
	if err != nil {
		t.Fatalf("SpoolExpiring: %v", err)
	}
	data, err := sp.Load(context.Background(), "alice", ref)
	if err != nil {
		t.Fatalf("Load before expiry: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("Load = %q, want payload", data)
	}
}

func TestLoadAfterExpiryFailsErrExpiredAndRegrantLoads(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 1024)
	_, ref, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), time.Nanosecond)
	if err != nil {
		t.Fatalf("SpoolExpiring: %v", err)
	}
	expiry, ok := sp.GrantExpiry(ref)
	if !ok {
		t.Fatal("GrantExpiry ok = false, want true")
	}
	awaitPast(expiry)
	_, err = sp.Load(context.Background(), "alice", ref)
	if !errors.Is(err, spool.ErrExpired) {
		t.Fatalf("Load after expiry = %v, want errors.Is ErrExpired", err)
	}
	if errors.Is(err, spool.ErrUnknownRef) {
		t.Fatalf("Load after expiry = %v, must not read as ErrUnknownRef", err)
	}
	_, ref2, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), time.Minute)
	if err != nil {
		t.Fatalf("re-grant after expiry: %v", err)
	}
	if _, err := sp.Load(context.Background(), "alice", ref2); err != nil {
		t.Fatalf("Load on the re-granted ref: %v", err)
	}
}

func TestLoadWrongPrincipalBeforeExpiryCheck(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 1024)
	_, ref, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), time.Hour)
	if err != nil {
		t.Fatalf("SpoolExpiring: %v", err)
	}
	if err := sp.Expire(ref); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	_, err = sp.Load(context.Background(), "bob", ref)
	if !errors.Is(err, spool.ErrWrongPrincipal) {
		t.Fatalf("Load under wrong principal on an expired grant = %v, want ErrWrongPrincipal first", err)
	}
}

func TestSpoolExpiringNonPositiveTTLFailsBeforeWrite(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 1024)
	for _, ttl := range []time.Duration{0, -time.Second} {
		_, _, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), ttl)
		if !errors.Is(err, spool.ErrInvalidExpiry) {
			t.Fatalf("SpoolExpiring(ttl %v) = %v, want errors.Is ErrInvalidExpiry", ttl, err)
		}
	}
	if store.putCalls != 0 {
		t.Fatalf("store.putCalls = %d, want 0: the ttl check runs before any write", store.putCalls)
	}
}

func TestExpireMarksAndRejectsUnknownRef(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 1024)
	_, ref, err := sp.Spool(context.Background(), "alice", []byte("payload"))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	if err := sp.Expire(ref); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if _, err := sp.Load(context.Background(), "alice", ref); !errors.Is(err, spool.ErrExpired) {
		t.Fatalf("Load after Expire = %v, want errors.Is ErrExpired", err)
	}
	if err := sp.Expire("ref-missing"); !errors.Is(err, spool.ErrUnknownRef) {
		t.Fatalf("Expire on unknown ref = %v, want errors.Is ErrUnknownRef", err)
	}
}

func TestGrantExpiryReports(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 1024)
	before := time.Now()
	_, ref, err := sp.SpoolExpiring(context.Background(), "alice", []byte("payload"), time.Hour)
	if err != nil {
		t.Fatalf("SpoolExpiring: %v", err)
	}
	got, ok := sp.GrantExpiry(ref)
	if !ok {
		t.Fatal("GrantExpiry ok = false, want true for a live grant")
	}
	if got.Before(before.Add(time.Hour)) {
		t.Fatalf("GrantExpiry = %v, want at least now plus the ttl", got)
	}
	if _, ok := sp.GrantExpiry("ref-missing"); ok {
		t.Fatal("GrantExpiry ok = true for an unknown ref, want false")
	}
	_, plainRef, err := sp.Spool(context.Background(), "alice", []byte("other"))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	plainExpiry, ok := sp.GrantExpiry(plainRef)
	if !ok || !plainExpiry.IsZero() {
		t.Fatalf("GrantExpiry on a plain grant = %v, %v, want zero time and true", plainExpiry, ok)
	}
}

func TestExpiredGrantFreesBudgetLazily(t *testing.T) {
	store := newFakeStore()
	sp, _ := spool.NewSpool(store, 200)
	if _, expRef, err := sp.SpoolExpiring(context.Background(), "alice", []byte("a-payload"), time.Hour); err != nil {
		t.Fatalf("SpoolExpiring: %v", err)
	} else if err := sp.Expire(expRef); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	_, keepRef, err := sp.Spool(context.Background(), "alice", []byte("b-payload"))
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	store.mu.Lock()
	expiredRef := ""
	for ref, blob := range store.blobs {
		if string(blob) == "a-payload" {
			expiredRef = ref
		}
	}
	store.mu.Unlock()
	if _, err := sp.Load(context.Background(), "alice", expiredRef); !errors.Is(err, spool.ErrExpired) {
		t.Fatalf("Load on the expired grant = %v, want ErrExpired", err)
	}
	if _, _, err := sp.Spool(context.Background(), "alice", []byte("c-payload")); err != nil {
		t.Fatalf("Spool after the expiry drop = %v, want the freed budget to fit it", err)
	}
	if _, err := sp.Load(context.Background(), "alice", keepRef); err != nil {
		t.Fatalf("Load on the untouched grant = %v, want nil: no eviction ran", err)
	}
}
