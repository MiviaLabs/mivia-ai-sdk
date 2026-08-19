// Package secretpath matches a filesystem path against a configured
// list of glob-style secret path patterns, so a caller can decide
// whether a path holds sensitive content before it reads, writes, or
// logs it. Matches is a pure string decision; it never touches the
// filesystem.
package secretpath
