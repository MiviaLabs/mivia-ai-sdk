# Example: the envelope flow

This walkthrough shows one message's life: create, sign, encode,
decode, verify, and then tamper with a field value. The code is
illustrative. It is a PoC sketch, not a buildable package. Runnable
examples are future work; a runnable package would need a policy row,
an API lock, and a plan.

## The program

```go
package main

import (
	"crypto/ed25519"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

func main() {
	_, key, _ := ed25519.GenerateKey(nil)

	msg := envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "task-42",
		Intent:     envelope.IntentRequest,
		Epistemic:  envelope.EpistemicInferred,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "Summarize the config loading path.",
	}

	// Sign sets the signer and the signature.
	signed, err := envelope.Sign(key, msg)
	if err != nil {
		fmt.Println("sign:", err)
	}

	// Encode validates, then serializes to JSON.
	data, err := signed.Encode()
	if err != nil {
		fmt.Println("encode:", err)
	}

	// Decode parses the JSON, then validates.
	got, err := envelope.Decode(data)
	if err != nil {
		fmt.Println("decode:", err)
	}

	// The signature matches the content.
	if err := got.VerifySignature(); err != nil {
		fmt.Println("verify:", err)
	}

	// Tamper with a field value: the JSON stays valid.
	got.Payload = "A different payload."

	// The tampered message still encodes and decodes.
	tampered, err := got.Encode()
	if err != nil {
		fmt.Println("encode tampered:", err)
	}
	again, err := envelope.Decode(tampered)
	if err != nil {
		fmt.Println("decode tampered:", err)
	}

	// The content changed, so the signature no longer matches.
	if err := again.VerifySignature(); err != nil {
		fmt.Println("verify tampered:", err)
	}
}
```

## What the program shows

The original message passes every step. After the payload changes,
the message still encodes and decodes: the JSON stays valid. The
signature covers the canonical JSON of every field except itself, so
any value change fails verification. Structural tampering breaks the
JSON and fails Decode instead; this example changes a value on
purpose.
