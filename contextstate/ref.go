package contextstate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashPrefix prefixes every canonical content address. The only
// place the literal may appear; semgrep enforces this.
const HashPrefix = "sha256:"

// digestHexLen is the length of a hex-encoded SHA-256 digest.
const digestHexLen = 64

// Digest returns the SHA-256 of the ordered concatenation of chunks,
// as 64 lowercase hex characters. The namespace and owner fields
// never mix in.
func Digest(chunks ...[]byte) string {
	sum := sha256.New()
	for _, chunk := range chunks {
		sum.Write(chunk)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Mint returns the canonical content address of the concatenated
// chunks: HashPrefix plus Digest. envelope.ContextRef delegates here.
// Minting over the concatenation of chunks equals minting over the
// concatenated bytes.
func Mint(chunks ...[]byte) string {
	return HashPrefix + Digest(chunks...)
}

// IsRef reports whether ref is the canonical form: HashPrefix, then
// exactly 64 lowercase hex characters. No surrounding whitespace is
// tolerated.
func IsRef(ref string) bool {
	hexPart, ok := strings.CutPrefix(ref, HashPrefix)
	return ok && isLowerHex(hexPart, digestHexLen)
}

// isLowerHex reports whether s is exactly n lowercase hex chars.
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
