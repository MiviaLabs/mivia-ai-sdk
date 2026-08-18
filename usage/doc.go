// Package usage gives a caller a per-session running total of
// provider.Usage. A caller calls Record once per completed model
// call and reads the accumulated total for that session at any time.
// usage adds no gate and no policy; it only counts.
//
// Map: accumulator.go = Accumulator, New, Record, Total, Reset, and
// the sentinel error ErrBlankSessionID. Rationale:
// ../docs/plans/usage.md.
// Contribution rules: ../AGENTS.md.
package usage
