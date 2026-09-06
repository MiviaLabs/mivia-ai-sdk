package contextref

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashPrefix prefixes every canonical content address.
const HashPrefix = "sha256:"

// digestHexLen is the length of a hex-encoded SHA-256 digest.
const digestHexLen = 64

// Digest returns the SHA-256 of the concatenated chunks as 64 lowercase hex characters.
func Digest(chunks ...[]byte) string {
	sum := sha256.New()
	for _, chunk := range chunks {
		sum.Write(chunk)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Mint returns HashPrefix plus Digest over the concatenated chunks.
func Mint(chunks ...[]byte) string {
	return HashPrefix + Digest(chunks...)
}

// IsRef reports whether ref matches HashPrefix followed by 64 lowercase hex characters.
func IsRef(ref string) bool {
	hexPart, ok := strings.CutPrefix(ref, HashPrefix)
	return ok && isLowerHex(hexPart, digestHexLen)
}

// isLowerHex reports whether s is exactly n lowercase hex characters.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
