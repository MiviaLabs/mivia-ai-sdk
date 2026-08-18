// Package durablefence is a conformance-test kit, built on
// testing.TB. No production code may import it; it exists to run
// inside another package's own _test subdirectory, wired against that
// package's real claim, takeover, and fence implementation.
//
// scenario.go defines Scenario, the caller-supplied function set under
// test, its Validate method, and ErrIncompleteScenario. checks.go
// defines the Check* functions and RunAll, which runs every check
// against one Scenario.
package durablefence
