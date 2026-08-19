package schema_test

import (
	"fmt"
	"sync"
	"testing"
)

// TestValidateConcurrentUse runs many goroutines calling Validate on
// one shared *Compiled value built once, each with its own payload, a
// mix of matching and schema-violating payloads. Run with -race: no
// data race and no incorrect verdict proves the concurrent-use claim
// in Validate's doc comment.
func TestValidateConcurrentUse(t *testing.T) {
	c := compileFixture(t, simpleObjectSchema)

	const goroutines = 64
	const perGoroutine = 32

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				matching := (id+i)%2 == 0
				var payload []byte
				if matching {
					payload = []byte(fmt.Sprintf(`{"name": "worker-%d-%d"}`, id, i))
				} else {
					payload = []byte(`{}`)
				}
				err := c.Validate(payload)
				if matching && err != nil {
					t.Errorf("goroutine %d iteration %d: matching payload got error %v", id, i, err)
				}
				if !matching && err == nil {
					t.Errorf("goroutine %d iteration %d: violating payload got no error", id, i)
				}
			}
		}(g)
	}
	wg.Wait()
}
