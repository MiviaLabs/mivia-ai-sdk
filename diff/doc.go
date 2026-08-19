// Package diff produces a bounded unified line diff between two byte
// slices, so tool output that shows a file change never grows past a
// caller's line budget.
package diff
