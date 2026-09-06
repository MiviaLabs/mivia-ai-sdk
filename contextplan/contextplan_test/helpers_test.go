package contextplan_test

import (
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// byteEstimator counts one token per content byte across every
// message; deterministic for budget arithmetic in tests. A non-nil
// err makes every EstimateTokens call fail.
type byteEstimator struct{ err error }

// EstimateTokens sums the byte length of every message's Content.
func (b byteEstimator) EstimateTokens(req provider.Request) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}
