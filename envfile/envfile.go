package envfile

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// keyPattern matches a valid dotenv key: a letter or underscore,
// followed by letters, digits, or underscores.
var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads the dotenv file at path and returns its KEY=VALUE pairs
// as a map. Load reads the file and delegates the parse to LoadBytes,
// so the two share one parser and one error set. Load returns a
// wrapped os.ErrNotExist when path does not exist. No returned error
// ever contains a parsed value.
func Load(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("envfile: %w", err)
	}
	return LoadBytes(data)
}

// LoadBytes parses an already-read dotenv body and returns its
// KEY=VALUE pairs as a map. A blank line and a line starting with '#'
// (after leading whitespace) skip. A value may be unquoted,
// single-quoted, or double-quoted; a double-quoted value decodes \n,
// \t, \\, and \". Only the first '=' on a line splits key from value,
// so a literal '=' in an unquoted or quoted value passes through. A
// duplicate key keeps its last value. LoadBytes(nil) returns an empty
// map and a nil error. No returned error ever contains a parsed
// value.
func LoadBytes(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, err := parseLine(trimmed, lineNo)
		if err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, nil
}

// parseLine splits one non-blank, non-comment dotenv line into its
// key and value.
func parseLine(line string, lineNo int) (string, string, error) {
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", fmt.Errorf("envfile: line %d: missing '='", lineNo)
	}
	key := strings.TrimSpace(line[:idx])
	if !keyPattern.MatchString(key) {
		return "", "", fmt.Errorf("envfile: line %d: invalid key", lineNo)
	}
	value, err := parseValue(line[idx+1:], lineNo)
	if err != nil {
		return "", "", err
	}
	return key, value, nil
}

// parseValue decodes the raw text after '=' into a value, handling
// quoting, escapes, and a trailing comment on an unquoted value.
func parseValue(raw string, lineNo int) (string, error) {
	s := strings.TrimLeft(raw, " \t")
	if len(s) > 0 && (s[0] == '\'' || s[0] == '"') {
		return parseQuotedValue(s, lineNo)
	}
	return parseUnquotedValue(s), nil
}

// parseQuotedValue decodes a single- or double-quoted value, checking
// the closing quote exists and only whitespace or a comment follows
// it.
func parseQuotedValue(s string, lineNo int) (string, error) {
	quote := s[0]
	closeIdx := -1
	for i := 1; i < len(s); i++ {
		if quote == '"' && s[i] == '\\' && i+1 < len(s) {
			i++
			continue
		}
		if s[i] == quote {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return "", fmt.Errorf("envfile: line %d: unterminated quote", lineNo)
	}
	inner := s[1:closeIdx]
	rest := strings.TrimSpace(s[closeIdx+1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", fmt.Errorf("envfile: line %d: trailing content after quoted value", lineNo)
	}
	if quote == '"' {
		unescaped, err := unescapeDouble(inner)
		if err != nil {
			return "", fmt.Errorf("envfile: line %d: invalid escape sequence", lineNo)
		}
		return unescaped, nil
	}
	return inner, nil
}

// parseUnquotedValue trims an unquoted value and strips a trailing
// comment, where a comment starts at a '#' that begins the value or
// follows whitespace.
func parseUnquotedValue(s string) string {
	cut := len(s)
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			cut = i
			break
		}
	}
	return strings.TrimSpace(s[:cut])
}

// unescapeDouble decodes \n, \t, \\, and \" inside a double-quoted
// value.
func unescapeDouble(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", errors.New("envfile: dangling escape")
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		default:
			return "", errors.New("envfile: unknown escape")
		}
	}
	return b.String(), nil
}
