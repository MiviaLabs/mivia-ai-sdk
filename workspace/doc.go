// Package workspace confines all filesystem access to one root
// directory, so a tool or agent that reads and writes files cannot
// escape its sandbox through path traversal or a symlink.
package workspace
