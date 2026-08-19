package envfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envfile"
)

// sensitive is the marker every error case embeds as a value. No
// error text may contain it.
const sensitive = "s3cr3t-token"

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// assertPairs compares a parsed map against want, key by key.
func assertPairs(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("parsed = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestLoadBytesParseCases(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    map[string]string
	}{
		{"nil input", nil, map[string]string{}},
		{"empty input", []byte(""), map[string]string{}},
		{"unquoted value", []byte("FOO=bar\n"), map[string]string{"FOO": "bar"}},
		{"single-quoted value", []byte("FOO='bar baz'\n"), map[string]string{"FOO": "bar baz"}},
		{"double-quoted value with escapes", []byte(`FOO="a\nb\tc\\d\"e"` + "\n"), map[string]string{"FOO": "a\nb\tc\\d\"e"}},
		{"comment line", []byte("# a comment\nFOO=bar\n"), map[string]string{"FOO": "bar"}},
		{"blank line", []byte("\nFOO=bar\n\n"), map[string]string{"FOO": "bar"}},
		{"trailing comment stripped only outside quotes", []byte("FOO=bar # comment\nBAR='baz # not comment'\n"), map[string]string{"FOO": "bar", "BAR": "baz # not comment"}},
		{"empty value", []byte("FOO=\n"), map[string]string{"FOO": ""}},
		{"whitespace around =", []byte("FOO = bar\n"), map[string]string{"FOO": "bar"}},
		{"duplicate key keeps last value", []byte("FOO=first\nFOO=second\n"), map[string]string{"FOO": "second"}},
		{"literal equals in value", []byte("KEY=a=b\n"), map[string]string{"KEY": "a=b"}},
		{"no trailing newline", []byte("FOO=bar"), map[string]string{"FOO": "bar"}},
		{"value is only a comment", []byte("FOO=# comment\n"), map[string]string{"FOO": ""}},
		{"tab before trailing comment", []byte("FOO=bar\t# comment\n"), map[string]string{"FOO": "bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := envfile.LoadBytes(tt.content)
			if err != nil {
				t.Fatalf("LoadBytes: %v", err)
			}
			if got == nil {
				t.Fatal("LoadBytes() = nil map with nil error")
			}
			assertPairs(t, got, tt.want)
		})
	}
}

func TestLoadBytesParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{"invalid key", []byte("1FOO=" + sensitive + "\n"), "invalid key"},
		{"empty key", []byte("=" + sensitive + "\n"), "invalid key"},
		{"unterminated quote", []byte("FOO=\"" + sensitive + "\n"), "unterminated quote"},
		{"missing equals", []byte("FOOBAR" + sensitive + "\n"), "missing '='"},
		{"trailing content after quoted value", []byte(`FOO="` + sensitive + `"baz` + "\n"), "trailing content after quoted value"},
		{"invalid escape sequence", []byte(`FOO="` + sensitive + `\xb"` + "\n"), "invalid escape sequence"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := envfile.LoadBytes(tt.content)
			if err == nil {
				t.Fatalf("LoadBytes() = %v, nil error, want error", got)
			}
			if got != nil {
				t.Errorf("LoadBytes() = %v on error, want nil map", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("LoadBytes() error = %v, want mention of %q", err, tt.want)
			}
			if strings.Contains(err.Error(), sensitive) {
				t.Errorf("LoadBytes() error = %v, must not contain parsed value", err)
			}
		})
	}
}

func TestLoadBytesErrorNeverContainsValue(t *testing.T) {
	contents := [][]byte{
		[]byte("1FOO=" + sensitive + "\n"),
		[]byte("FOO=\"" + sensitive + "\n"),
	}
	for _, content := range contents {
		_, err := envfile.LoadBytes(content)
		if err == nil {
			t.Fatal("LoadBytes() = nil error, want error")
		}
		if strings.Contains(err.Error(), sensitive) {
			t.Errorf("LoadBytes() error = %v, must not contain parsed value", err)
		}
	}
}

func TestLoadBytesCRLF(t *testing.T) {
	gotCRLF, err := envfile.LoadBytes([]byte("FOO=bar\r\nBAZ=qux\r\n"))
	if err != nil {
		t.Fatalf("LoadBytes(CRLF): %v", err)
	}
	gotLF, err := envfile.LoadBytes([]byte("FOO=bar\nBAZ=qux\n"))
	if err != nil {
		t.Fatalf("LoadBytes(LF): %v", err)
	}
	assertPairs(t, gotCRLF, gotLF)

	// A blank or comment line in CRLF form is the case the trailing
	// "\r" trim exists for. On a KEY=VALUE line the value parser's own
	// TrimSpace hides the trim, so only these lines discriminate.
	got, err := envfile.LoadBytes([]byte("FOO=bar\r\n\r\n# note\r\nBAZ=qux\r\n"))
	if err != nil {
		t.Fatalf("LoadBytes(CRLF with blank and comment lines): %v", err)
	}
	assertPairs(t, got, map[string]string{"FOO": "bar", "BAZ": "qux"})
}

// TestLoadReadsFileAndDelegates pins that Load reads the file and
// returns exactly what LoadBytes returns for the same bytes.
func TestLoadReadsFileAndDelegates(t *testing.T) {
	content := "FOO=bar\n# a comment\nBAR='baz # not comment'\nKEY=a=b\n"
	path := writeTemp(t, content)

	got, err := envfile.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want, err := envfile.LoadBytes([]byte(content))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	assertPairs(t, got, want)
	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q, want bar", got["FOO"])
	}
}

// TestLoadDelegatesParseError pins that a parse failure reaches the
// caller through Load with the shared parser's message.
func TestLoadDelegatesParseError(t *testing.T) {
	path := writeTemp(t, "1FOO="+sensitive+"\n")
	got, err := envfile.Load(path)
	if err == nil {
		t.Fatalf("Load() = %v, nil error, want error", got)
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("Load() error = %v, want mention of invalid key", err)
	}
	if strings.Contains(err.Error(), sensitive) {
		t.Errorf("Load() error = %v, must not contain parsed value", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := envfile.Load(filepath.Join(t.TempDir(), "missing.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) error = %v, want wrapped os.ErrNotExist", err)
	}
}

// FuzzLoad feeds arbitrary dotenv text to Load. It must never panic,
// a successful parse must reproduce the same result on a second parse
// of the same file, and Load must agree with LoadBytes on the same
// bytes, since Load only reads the file and delegates.
// Run: go test -fuzz=FuzzLoad ./envfile/envfile_test/
func FuzzLoad(f *testing.F) {
	seeds := []string{
		"FOO=bar\n",
		"FOO='bar baz'\n",
		"FOO=\"a\\nb\\tc\\\\d\\\"e\"\n",
		"# a comment\nFOO=bar\n",
		"\nFOO=bar\n\n",
		"FOO=bar # comment\nBAR='baz # not comment'\n",
		"FOO=\n",
		"FOO = bar\n",
		"1FOO=bar\n",
		"FOO=\"bar\n",
		"FOO=first\nFOO=second\n",
		"KEY=a=b\n",
		"FOO=bar\r\nBAZ=qux\r\n",
		"FOOBAR\n",
		"FOO=\"bar\"baz\n",
		"FOO=\"a\\xb\"\n",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		path := writeTemp(t, content)
		got, err := envfile.Load(path)
		direct, directErr := envfile.LoadBytes([]byte(content))
		if (err == nil) != (directErr == nil) {
			t.Fatalf("Load error = %v, LoadBytes error = %v, want the same outcome", err, directErr)
		}
		if err != nil {
			if err.Error() != directErr.Error() {
				t.Fatalf("Load error = %q, LoadBytes error = %q, want the same text", err, directErr)
			}
			return
		}
		if got == nil {
			t.Fatal("Load() = nil map with nil error")
		}
		fuzzAssertSame(t, "LoadBytes", got, direct)

		again, err := envfile.Load(path)
		if err != nil {
			t.Fatalf("Load() succeeded once, failed on repeat: %v", err)
		}
		fuzzAssertSame(t, "repeat Load", got, again)
	})
}

// fuzzAssertSame fails the fuzz case when two parses disagree.
func fuzzAssertSame(t *testing.T, label string, got, other map[string]string) {
	t.Helper()
	if len(other) != len(got) {
		t.Fatalf("Load() and %s disagree: %v, %v", label, got, other)
	}
	for k, v := range got {
		if other[k] != v {
			t.Fatalf("Load() and %s disagree at key %s: %q, %q", label, k, v, other[k])
		}
	}
}
