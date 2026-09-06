package contextstate

import (
	"github.com/MiviaLabs/mivia-ai-sdk/contextref"
)

// HashPrefix prefixes every canonical content address.
const HashPrefix = "sha256:"

// Digest returns the SHA-256 of the concatenated chunks as 64 lowercase hex characters.
func Digest(chunks ...[]byte) string {
	return contextref.Digest(chunks...)
}

// Mint returns HashPrefix plus Digest over the concatenated chunks.
func Mint(chunks ...[]byte) string {
	return contextref.Mint(chunks...)
}

// IsRef reports whether ref matches HashPrefix followed by 64 lowercase hex characters.
func IsRef(ref string) bool {
	return contextref.IsRef(ref)
}
