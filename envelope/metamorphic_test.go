package envelope

import (
	"bytes"
	"crypto/ed25519"
	"reflect"
	"testing"
)

// TestMetamorphicDecodeEncodeHashStable pins the property: decoding a
// message then re-encoding it preserves Hash(). Decode unmarshals into
// the fixed-field Message struct, so json.Marshal always emits fields
// in struct order regardless of input key order.
func TestMetamorphicDecodeEncodeHashStable(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	cases := map[string]func() Message{
		"assert inferred unsigned": func() Message {
			return validMessage()
		},
		"query assumed with prev hash": func() Message {
			m := validMessage()
			m.Intent = IntentQuery
			m.Epistemic = EpistemicAssumed
			prev := validMessage()
			m.PrevHash = prev.Hash()
			return m
		},
		"escalate verified signed": func() Message {
			m := validMessage()
			m.Intent = IntentEscalate
			m.Epistemic = EpistemicVerified
			m.Provenance = Provenance{
				Source:   "tool:grep",
				Evidence: []string{ContextRef("evidence")},
			}
			signed, err := Sign(key, m)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		},
		"retract untrusted input signed": func() Message {
			m := validMessage()
			m.Intent = IntentRetract
			m.InReplyTo = "msg-0"
			m.Epistemic = EpistemicUntrustedInput
			signed, err := Sign(key, m)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			original := build()
			wantHash := original.Hash()

			data, err := original.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoded, err := Decode(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := decoded.Hash(); got != wantHash {
				t.Fatalf("decode then hash = %q, want %q", got, wantHash)
			}

			reencoded, err := decoded.Encode()
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			redecoded, err := Decode(reencoded)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if got := redecoded.Hash(); got != wantHash {
				t.Fatalf("re-decode then hash = %q, want %q", got, wantHash)
			}
		})
	}
}

// TestMetamorphicThreadReorderBreaksVerify pins the property: swapping
// two messages in a valid thread always breaks VerifyThread. A swap
// either breaks the first-message PrevHash=="" rule or breaks a middle
// PrevHash match, since swapped messages carry different Hash values.
func TestMetamorphicThreadReorderBreaksVerify(t *testing.T) {
	buildChain := func(n int) []Message {
		msgs := make([]Message, n)
		var prev *Message
		for i := 0; i < n; i++ {
			m := chainMessage(idFor(i), prev)
			msgs[i] = m
			prev = &msgs[i]
		}
		return msgs
	}

	cases := []struct {
		name   string
		length int
		i, j   int
	}{
		{"length 3 adjacent swap 0-1", 3, 0, 1},
		{"length 3 non-adjacent swap 0-2", 3, 0, 2},
		{"length 4 adjacent swap 1-2", 4, 1, 2},
		{"length 4 non-adjacent swap 0-3", 4, 0, 3},
		{"length 5 adjacent swap 2-3", 5, 2, 3},
		{"length 5 non-adjacent swap 1-4", 5, 1, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := buildChain(tc.length)
			if err := VerifyThread(chain); err != nil {
				t.Fatalf("baseline chain rejected: %v", err)
			}
			swapped := append([]Message(nil), chain...)
			swapped[tc.i], swapped[tc.j] = swapped[tc.j], swapped[tc.i]
			if err := VerifyThread(swapped); err == nil {
				t.Fatalf("swap of positions %d and %d must break VerifyThread", tc.i, tc.j)
			}
		})
	}
}

// idFor names a chain message deterministically by its position.
func idFor(i int) string {
	return "chain-msg-" + string(rune('a'+i))
}

// TestMetamorphicDecodeRoundTrips pins the property: any input Decode
// accepts round-trips through Encode and Decode again to an equal
// Message with a matching Hash. Unknown fields are dropped by
// json.Unmarshal into a typed struct, and Validate runs identically on
// both decodes.
func TestMetamorphicDecodeRoundTrips(t *testing.T) {
	base := []byte(`{"version":"v1","id":"m1","thread_id":"t1","intent":"query","epistemic":"assumed","confidence":0.25,"payload":"q?"}`)
	reordered := []byte(`{"thread_id":"t1","version":"v1","payload":"q?","confidence":0.25,"epistemic":"assumed","id":"m1","intent":"query"}`)
	whitespace := []byte(`{
		"version": "v1",
		"id":      "m1",
		"thread_id": "t1",
		"intent": "query",
		"epistemic": "assumed",
		"confidence": 0.25,
		"payload": "q?"
	}`)
	unknownField := []byte(`{"version":"v1","id":"m1","thread_id":"t1","intent":"query","epistemic":"assumed","confidence":0.25,"payload":"q?","future_field":{"nested":true}}`)

	cases := map[string][]byte{
		"reordered keys":   reordered,
		"added whitespace": whitespace,
		"unknown field":    unknownField,
		"canonical":        base,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			first, err := Decode(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			encoded, err := first.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			second, err := Decode(encoded)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("round trip mismatch: %+v vs %+v", first, second)
			}
			if first.Hash() != second.Hash() {
				t.Fatalf("hash mismatch: %q vs %q", first.Hash(), second.Hash())
			}
			// The unknown-field case must actually drop the field: prove the
			// re-encoded bytes never carry it forward.
			if name == "unknown field" && bytes.Contains(encoded, []byte("future_field")) {
				t.Fatal("re-encoded message must not carry the unknown field forward")
			}
		})
	}
}
