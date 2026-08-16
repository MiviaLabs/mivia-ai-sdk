package envelope

import (
	"testing"
)

func validMessage() Message {
	return Message{
		Version:    Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     IntentAssert,
		Epistemic:  EpistemicInferred,
		Confidence: 0.6,
		Provenance: Provenance{Source: "model:self"},
		Payload:    "The API returns JSON.",
	}
}

func TestValidateAcceptsValidMessage(t *testing.T) {
	if err := validMessage().Validate(); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
}

func TestValidateRejectsBadFields(t *testing.T) {
	cases := map[string]func(*Message){
		"bad version":    func(m *Message) { m.Version = "v0" },
		"missing id":     func(m *Message) { m.ID = "" },
		"missing thread": func(m *Message) { m.ThreadID = "" },
		"self reply":     func(m *Message) { m.InReplyTo = m.ID },
		"retract no target": func(m *Message) {
			m.Intent = IntentRetract
			m.InReplyTo = ""
		},
		"verified no source": func(m *Message) {
			m.Epistemic = EpistemicVerified
			m.Provenance = Provenance{Evidence: []string{ContextRef("e")}}
		},
		"verified no evidence": func(m *Message) {
			m.Epistemic = EpistemicVerified
			m.Provenance = Provenance{Source: "tool:grep"}
		},
		"bad evidence ref": func(m *Message) {
			m.Provenance.Evidence = []string{"sha256:xyz"}
		},
		"bad intent":      func(m *Message) { m.Intent = "yell" },
		"bad epistemic":   func(m *Message) { m.Epistemic = "sure" },
		"confidence > 1":  func(m *Message) { m.Confidence = 1.5 },
		"confidence < 0":  func(m *Message) { m.Confidence = -0.1 },
		"negative budget": func(m *Message) { m.CostBudget = -5 },
		"empty payload":   func(m *Message) { m.Payload = "  " },
		"bad ref scheme":  func(m *Message) { m.ContextRefs = []string{"md5:abc"} },
		"short ref":       func(m *Message) { m.ContextRefs = []string{"sha256:abc"} },
		"uppercase ref": func(m *Message) {
			m.ContextRefs = []string{"sha256:" + "ABCDEF0123456789" + "ABCDEF0123456789" + "ABCDEF0123456789" + "ABCDEF0123456789"}
		},
		"bad prev hash": func(m *Message) { m.PrevHash = "sha256:xyz" },
		"negative hops": func(m *Message) { m.MaxHops = -1 },
		"hops exceeded": func(m *Message) {
			m.MaxHops = 1
			m.Provenance.Chain = []string{"agent-a", "agent-b"}
		},
		"signer without signature": func(m *Message) {
			m.Signer = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		},
		"signature without signer": func(m *Message) {
			m.Signature = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		},
		"short signer": func(m *Message) {
			m.Signer = "abcd"
			m.Signature = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := validMessage()
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestVerifiedWithSourceAndEvidenceIsValid(t *testing.T) {
	m := validMessage()
	m.Epistemic = EpistemicVerified
	m.Provenance = Provenance{
		Source:   "tool:grep",
		Evidence: []string{ContextRef("grep output of config loader")},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("verified with source and evidence rejected: %v", err)
	}
}

func TestRetractWithTargetIsValid(t *testing.T) {
	m := validMessage()
	m.Intent = IntentRetract
	m.InReplyTo = "msg-0"
	if err := m.Validate(); err != nil {
		t.Fatalf("retract with target rejected: %v", err)
	}
}

func TestEscalateIsValid(t *testing.T) {
	m := validMessage()
	m.Intent = IntentEscalate
	m.Payload = "Mean confidence 0.55; route to a human reviewer."
	if err := m.Validate(); err != nil {
		t.Fatalf("escalate rejected: %v", err)
	}
}

func TestMaxHopsWithinLimitIsValid(t *testing.T) {
	m := validMessage()
	m.MaxHops = 2
	m.Provenance.Chain = []string{"agent-a"}
	if err := m.Validate(); err != nil {
		t.Fatalf("chain within max_hops rejected: %v", err)
	}
}

func TestContextRefRoundTrip(t *testing.T) {
	ref := ContextRef("shared context blob")
	m := validMessage()
	m.ContextRefs = []string{ref}
	if err := m.Validate(); err != nil {
		t.Fatalf("context ref rejected: %v", err)
	}
}

func TestHashIsDeterministicAndValidatesAsPrevHash(t *testing.T) {
	prev := validMessage()
	m := validMessage()
	m.ID = "msg-2"
	m.InReplyTo = prev.ID
	m.PrevHash = prev.Hash()
	if m.PrevHash != prev.Hash() {
		t.Fatal("Hash must be deterministic")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("message with prev_hash rejected: %v", err)
	}
	// Any change to the message changes its hash.
	other := prev
	other.Payload = "Different."
	if other.Hash() == prev.Hash() {
		t.Fatal("Hash must change with content")
	}
}

func TestRequiresAck(t *testing.T) {
	m := validMessage()
	if m.RequiresAck() {
		t.Fatal("plain assert must not require ack")
	}
	m.Intent = IntentRequest
	if !m.RequiresAck() {
		t.Fatal("request must require ack")
	}
	m = validMessage()
	m.AckRequired = true
	if !m.RequiresAck() {
		t.Fatal("AckRequired flag must force ack")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := validMessage()
	m.ContextRefs = []string{ContextRef("blob")}
	data, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != m.ID || got.Payload != m.Payload {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestDecodeIgnoresUnknownFields(t *testing.T) {
	data := []byte(`{"version":"v1","id":"m","thread_id":"t","intent":"query","epistemic":"assumed","confidence":0.1,"payload":"q?","future_field":42}`)
	if _, err := Decode(data); err != nil {
		t.Fatalf("unknown field must be ignored: %v", err)
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	if _, err := Decode([]byte(`{"id":"x"}`)); err == nil {
		t.Fatal("expected decode to reject invalid message")
	}
}

func TestGroupAddressing(t *testing.T) {
	m := validMessage()
	m.Room = "platform-team"
	m.To = []string{"agent-a", "agent-b"}
	if err := m.Validate(); err != nil {
		t.Fatalf("group message rejected: %v", err)
	}

	dup := m
	dup.To = []string{"agent-a", "agent-a"}
	if err := dup.Validate(); err == nil {
		t.Fatal("duplicate recipients must fail")
	}

	empty := m
	empty.To = []string{"  "}
	if err := empty.Validate(); err == nil {
		t.Fatal("empty recipient must fail")
	}
}

func TestAckFlow(t *testing.T) {
	m := validMessage()
	m.Intent = IntentRequest

	ack, err := NewAck(m, "agent-b", "You want the report by Friday.")
	if err != nil {
		t.Fatalf("new ack: %v", err)
	}
	if ack.Status != AckPending {
		t.Fatalf("initial status = %q, want pending", ack.Status)
	}
	if err := ack.Validate(); err != nil {
		t.Fatalf("pending ack rejected: %v", err)
	}

	confirmed := ack.Confirm()
	if err := confirmed.Validate(); err != nil {
		t.Fatalf("confirmed ack rejected: %v", err)
	}

	corrected := ack.Correct("By Thursday, not Friday.")
	if err := corrected.Validate(); err != nil {
		t.Fatalf("corrected ack rejected: %v", err)
	}
	// Confirm after Correct must clear the stale correction.
	if got := corrected.Confirm(); got.Correction != "" {
		t.Fatal("Confirm must clear Correction")
	}

	bad := ack.Correct("")
	if err := bad.Validate(); err == nil {
		t.Fatal("corrected ack without correction must fail validation")
	}
}

func TestNewAckRequiresFromAndRestatement(t *testing.T) {
	if _, err := NewAck(validMessage(), "", "restatement"); err == nil {
		t.Fatal("empty from must fail")
	}
	if _, err := NewAck(validMessage(), "agent-b", "  "); err == nil {
		t.Fatal("empty restatement must fail")
	}
}
