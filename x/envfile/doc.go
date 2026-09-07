// Package envfile loads a dotenv file into a map.
//
// Load reads a file at path and returns its KEY=VALUE pairs as a map.
// Each error names a line number and a key; no parsed value appears.
//
// A blank line and a line starting with '#' (after leading whitespace)
// skip. The first '=' on a line splits the key from the value. Keys
// match the pattern [A-Za-z_][A-Za-z0-9_]*.
//
// A line may start with the lowercase keyword 'export' followed by
// whitespace; the keyword is stripped before key validation. A value
// may be unquoted, single-quoted, or double-quoted; a double-quoted
// value decodes '\n', '\t', '\\', and '\"'. A trailing '#' comment
// outside quotes is stripped.
package envfile
