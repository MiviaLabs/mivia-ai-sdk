package skills_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

// TestConcurrentAddDistinctNamesAllLand runs N goroutines each Add-ing
// a distinct name concurrently, then joins. A following Names call
// must find every one of the N names, proving concurrent Add calls
// all land.
func TestConcurrentAddDistinctNamesAllLand(t *testing.T) {
	r := skills.New()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("skill-%03d", i)
			s := skills.Skill{Name: name, Instructions: "x"}
			if err := r.Add(s); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	got := r.Names()
	if len(got) != n {
		t.Fatalf("Names() len = %d, want %d", len(got), n)
	}
	want := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		want[fmt.Sprintf("skill-%03d", i)] = true
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("Names() contains unexpected %q", name)
		}
	}
}

// TestConcurrentMatchRacesAdd races N Match calls for a shared query
// against N Add calls for N distinct new skills, under go test -race.
// No call may panic, and every Match call must return a result
// consistent with some point-in-time registry state: every returned
// skill's Triggers must actually contain the query.
func TestConcurrentMatchRacesAdd(t *testing.T) {
	r := skills.New()
	const n = 100
	const query = "shared-trigger"
	if err := r.Add(skills.Skill{Name: "seed", Instructions: "x", Triggers: []string{query}}); err != nil {
		t.Fatalf("Add(seed) error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			got := r.Match(query)
			for _, s := range got {
				found := false
				for _, trigger := range s.Triggers {
					if trigger == query {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Match(%s) returned %s without the query in Triggers", query, s.Name)
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("added-%03d", i)
			s := skills.Skill{Name: name, Instructions: "x"}
			if err := r.Add(s); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	got := r.Match(query)
	if len(got) != 1 || got[0].Name != "seed" {
		t.Fatalf("Match(%s) after wg.Wait = %v, want only [seed]", query, got)
	}
}

// TestConcurrentAddSameNameExactlyOneWins runs N goroutines all
// calling Add with the identical Skill{Name: "x", ...} concurrently.
// Exactly one call must return nil error; the rest must return
// ErrDuplicateName. A following Names call must find "x" exactly
// once.
func TestConcurrentAddSameNameExactlyOneWins(t *testing.T) {
	r := skills.New()
	const n = 100
	s := skills.Skill{Name: "x", Instructions: "shared instructions"}

	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	var nilCount, dupCount int
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			err := r.Add(s)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				nilCount++
				return
			}
			if errors.Is(err, skills.ErrDuplicateName) {
				dupCount++
				return
			}
			t.Errorf("Add(x) error = %v, want nil or ErrDuplicateName", err)
		}()
	}
	wg.Wait()

	if nilCount != 1 {
		t.Fatalf("nilCount = %d, want exactly 1", nilCount)
	}
	if dupCount != n-1 {
		t.Fatalf("dupCount = %d, want %d", dupCount, n-1)
	}

	names := r.Names()
	count := 0
	for _, name := range names {
		if name == "x" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Names() contains %q %d times, want exactly 1", "x", count)
	}
}
