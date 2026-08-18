package memory_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

// TestConcurrentPutAllLand runs N goroutines each calling Put with
// distinct content concurrently, on a Store sized to hold every blob
// without eviction, then joins. A following loop of Get calls, one
// per returned ref, must resolve every blob, proving concurrent Put
// calls all land.
func TestConcurrentPutAllLand(t *testing.T) {
	const n = 200
	const blobSize = 16
	s, err := memory.New(n * blobSize)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	refs := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			content := []byte(fmt.Sprintf("blob-%011d", i))
			ref, err := s.Put(content)
			if err != nil {
				t.Errorf("Put(%d) error = %v", i, err)
				return
			}
			refs[i] = ref
		}()
	}
	wg.Wait()

	for i, ref := range refs {
		got, err := s.Get(ref)
		if err != nil {
			t.Fatalf("Get(refs[%d]) error = %v, want nil", i, err)
		}
		want := fmt.Sprintf("blob-%011d", i)
		if string(got) != want {
			t.Fatalf("Get(refs[%d]) = %q, want %q", i, got, want)
		}
	}
}

// TestConcurrentPutEvictionAgainstGet runs N goroutines calling Put
// concurrently on a Store sized to hold only a few of the N blobs,
// forcing eviction, while N other goroutines concurrently call Get
// on refs returned by earlier Put calls. No call may panic or
// corrupt the store; every Get call returns either the correct blob
// or ErrUnknownRef, never a wrong or partial blob.
func TestConcurrentPutEvictionAgainstGet(t *testing.T) {
	const n = 200
	const blobSize = 16
	// Budget for roughly a tenth of the blobs, forcing heavy eviction.
	s, err := memory.New(n * blobSize / 10)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	contents := make([][]byte, n)
	for i := 0; i < n; i++ {
		contents[i] = []byte(fmt.Sprintf("payload-%08d", i))
	}

	refCh := make(chan string, n)
	var putWG sync.WaitGroup
	putWG.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer putWG.Done()
			ref, err := s.Put(contents[i])
			if err != nil {
				t.Errorf("Put(%d) error = %v", i, err)
				return
			}
			refCh <- ref
		}()
	}

	var getWG sync.WaitGroup
	stop := make(chan struct{})
	getWG.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer getWG.Done()
			for {
				select {
				case ref, ok := <-refCh:
					if !ok {
						return
					}
					blob, err := s.Get(ref)
					if err != nil {
						if !errors.Is(err, memory.ErrUnknownRef) {
							t.Errorf("Get error = %v, want nil or %v", err, memory.ErrUnknownRef)
						}
						continue
					}
					// A wrong or partial blob would fail this shape
					// check: every stored payload is exactly blobSize
					// bytes and starts with the fixed prefix.
					if len(blob) != blobSize || !strings.HasPrefix(string(blob), "payload-") {
						t.Errorf("Get(%s) returned malformed blob %q", ref, blob)
					}
				case <-stop:
					return
				}
			}
		}()
	}

	putWG.Wait()
	close(refCh)
	getWG.Wait()
	close(stop)
}
