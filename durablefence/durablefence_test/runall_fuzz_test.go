package durablefence_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// FuzzRunAllAgainstNonTestingT calls RunAll with a *testing.F, a real
// testing.TB that is not a *testing.T, proving RunAll falls back to
// calling each Check* function directly instead of wrapping it in a
// subtest when the caller's testing.TB carries no Run method of its
// own. It then hands F.Fuzz an empty target with no seed corpus, the
// call the testing package requires before a fuzz test may return.
func FuzzRunAllAgainstNonTestingT(f *testing.F) {
	ctx := context.Background()
	r := newReferenceClaim()
	durablefence.RunAll(f, ctx, r.scenario())
	f.Fuzz(func(*testing.T, []byte) {})
}
