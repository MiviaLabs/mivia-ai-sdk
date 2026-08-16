package envelope

import (
	"testing"
)

func chainMessage(id string, prev *Message) Message {
	m := validMessage()
	m.ID = id
	if prev != nil {
		m.PrevHash = prev.Hash()
	}
	return m
}

func TestVerifyThreadAcceptsValidChain(t *testing.T) {
	m1 := chainMessage("m1", nil)
	m2 := chainMessage("m2", &m1)
	m3 := chainMessage("m3", &m2)
	if err := VerifyThread([]Message{m1, m2, m3}); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
}

func TestVerifyThreadRejectsBadChains(t *testing.T) {
	m1 := chainMessage("m1", nil)
	m2 := chainMessage("m2", &m1)
	m3 := chainMessage("m3", &m2)

	cases := map[string][]Message{
		"empty": {},
		"first with parent": {func() Message {
			m := chainMessage("m0", &m1)
			return m
		}()},
		"reordered": {m1, m3, m2},
		"gap":       {m1, m3},
		"mixed threads": {m1, func() Message {
			x := chainMessage("x", &m1)
			x.ThreadID = "other"
			return x
		}()},
		"invalid message": {m1, func() Message {
			x := chainMessage("x", &m1)
			x.Payload = ""
			return x
		}()},
		"forged link": {m1, m2, func() Message {
			x := chainMessage("m3", &m1) // skips m2 but pretends to follow it
			x.ID = "m3"
			return x
		}()},
		"duplicate id": {m1, func() Message {
			// Repeats m1's ID with a valid prev_hash link.
			return chainMessage("m1", &m1)
		}()},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			if err := VerifyThread(msgs); err == nil {
				t.Fatal("expected thread verification to fail")
			}
		})
	}
}
