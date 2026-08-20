package agentrun_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
)

// TestArtifactsEncodeDecodeRoundTrip proves Encode and DecodeArtifacts
// round-trip every Get and History result. Encode and DecodeArtifacts
// do not exist before this addendum, so this test fails to compile
// until they ship.
func TestArtifactsEncodeDecodeRoundTrip(t *testing.T) {
	a := &agentrun.Artifacts{}
	a.SetRun("msg-1", "review", "reviewed:v1")
	a.SetRun("msg-2", "review", "reviewed:v2")
	a.SetRun("msg-3", "ship", "shipped:v1")

	data, err := a.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := agentrun.DecodeArtifacts(data)
	if err != nil {
		t.Fatalf("DecodeArtifacts: %v", err)
	}

	for _, step := range []string{"review", "ship"} {
		wantV, wantOK := a.Get(step)
		gotV, gotOK := decoded.Get(step)
		if gotV != wantV || gotOK != wantOK {
			t.Errorf("Get(%q) = %q,%v want %q,%v", step, gotV, gotOK, wantV, wantOK)
		}

		wantHist := a.History(step)
		gotHist := decoded.History(step)
		if len(gotHist) != len(wantHist) {
			t.Fatalf("History(%q) length = %d, want %d", step, len(gotHist), len(wantHist))
		}
		for i := range wantHist {
			if gotHist[i] != wantHist[i] {
				t.Errorf("History(%q)[%d] = %+v, want %+v", step, i, gotHist[i], wantHist[i])
			}
		}
	}
}

// TestArtifactsEncodeDecodeConcurrent proves Encode is safe to call
// concurrently with Set and SetRun, and that every Encode result
// decodes and passes Validate.
func TestArtifactsEncodeDecodeConcurrent(t *testing.T) {
	a := &agentrun.Artifacts{}
	const n = 100

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			step := "step-" + string(rune('a'+i%26))
			a.SetRun("msg", step, string(rune('a'+i%26)))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			data, err := a.Encode()
			if err != nil {
				t.Errorf("Encode: %v", err)
				return
			}
			decoded, err := agentrun.DecodeArtifacts(data)
			if err != nil {
				t.Errorf("DecodeArtifacts: %v", err)
				return
			}
			if err := decoded.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

// TestDecodeArtifactsMalformedJSON proves invalid JSON fails to decode.
func TestDecodeArtifactsMalformedJSON(t *testing.T) {
	_, err := agentrun.DecodeArtifacts([]byte("not json"))
	if err == nil {
		t.Fatal("DecodeArtifacts succeeded on malformed JSON, want an error")
	}
}

// TestDecodeArtifactsValueMismatchesLastRun proves a structurally
// valid document, where a step's current value differs from its last
// run's value, fails Validate.
func TestDecodeArtifactsValueMismatchesLastRun(t *testing.T) {
	data := []byte(`{"values":{"review":"stale"},"runs":{"review":[{"MessageID":"m1","Value":"fresh"}]}}`)
	_, err := agentrun.DecodeArtifacts(data)
	if !errors.Is(err, agentrun.ErrArtifactsInconsistent) {
		t.Fatalf("DecodeArtifacts error = %v, want ErrArtifactsInconsistent", err)
	}
}

// TestDecodeArtifactsValueWithoutRuns proves a structurally valid
// document, where a step holds a current value but zero recorded
// runs, fails Validate. This pins the invariant's other clause,
// distinct from a value mismatching its last run.
func TestDecodeArtifactsValueWithoutRuns(t *testing.T) {
	data := []byte(`{"values":{"review":"orphaned"},"runs":{}}`)
	_, err := agentrun.DecodeArtifacts(data)
	if !errors.Is(err, agentrun.ErrArtifactsInconsistent) {
		t.Fatalf("DecodeArtifacts error = %v, want ErrArtifactsInconsistent", err)
	}
}

// TestArtifactsEncodeEmptyValue proves Encode on a never-Set, non-nil
// Artifacts succeeds, and the decoded value reads as empty.
func TestArtifactsEncodeEmptyValue(t *testing.T) {
	a := &agentrun.Artifacts{}
	data, err := a.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := agentrun.DecodeArtifacts(data)
	if err != nil {
		t.Fatalf("DecodeArtifacts: %v", err)
	}
	if v, ok := decoded.Get("anything"); ok || v != "" {
		t.Fatalf("Get on decoded empty artifacts = %q,%v want empty,false", v, ok)
	}
	if hist := decoded.History("anything"); len(hist) != 0 {
		t.Fatalf("History on decoded empty artifacts = %v, want empty", hist)
	}
}

// TestArtifactsEncodeNilReceiver proves Encode on a true nil pointer,
// distinct from a non-nil zero value, succeeds and returns the JSON of
// an empty wireArtifacts, matching the nil-safe Get and History
// pattern.
func TestArtifactsEncodeNilReceiver(t *testing.T) {
	var a *agentrun.Artifacts
	data, err := a.Encode()
	if err != nil {
		t.Fatalf("Encode on nil receiver: %v", err)
	}

	decoded, err := agentrun.DecodeArtifacts(data)
	if err != nil {
		t.Fatalf("DecodeArtifacts: %v", err)
	}
	if v, ok := decoded.Get("anything"); ok || v != "" {
		t.Fatalf("Get on decoded nil-origin artifacts = %q,%v want empty,false", v, ok)
	}
}
