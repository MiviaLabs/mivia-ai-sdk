// Package workspace confines all filesystem access to one root
// directory, so a tool or agent that reads and writes files cannot
// escape its sandbox through path traversal or a symlink. The
// package also matches a filesystem path against a configured list
// of glob-style secret path patterns: Matcher.Match is a pure string
// decision, so a caller can decide whether a path holds sensitive
// content before it reads, writes, or logs it.
package workspace
