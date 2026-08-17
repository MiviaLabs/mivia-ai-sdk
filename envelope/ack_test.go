package envelope

import (
	"testing"
)

func TestAckEncodeDecodeRoundTrip(t *testing.T) {
	ack, err := NewAck(validMessage(), "agent-b", "You want X.")
	if err != nil {
		t.Fatalf("new ack: %v", err)
	}
	data, err := ack.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeAck(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != ack {
		t.Fatalf("round trip mismatch: %+v != %+v", got, ack)
	}
}

func TestDecodeAckRejectsInvalid(t *testing.T) {
	if _, err := DecodeAck([]byte(`{"message_id":"m"}`)); err == nil {
		t.Fatal("incomplete ack must fail")
	}
	if _, err := DecodeAck([]byte(`not json`)); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestAckEncodeRejectsInvalid(t *testing.T) {
	if _, err := (Ack{}).Encode(); err == nil {
		t.Fatal("empty ack must not encode")
	}
}

func TestNewAckRejectsEmptyMessageID(t *testing.T) {
	if _, err := NewAck(Message{}, "agent-b", "You want X."); err == nil {
		t.Fatal("ack for an empty message id must fail")
	}
}
