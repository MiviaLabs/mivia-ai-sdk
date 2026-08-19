package envfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envfile"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadParseCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
	}{
		{"unquoted value", "FOO=bar\n", map[string]string{"FOO": "bar"}},
		{"single-quoted value", "FOO='bar baz'\n", map[string]string{"FOO": "bar baz"}},
		{"double-quoted value with escapes", `FOO="a\nb\tc\\d\"e"` + "\n", map[string]string{"FOO": "a\nb\tc\\d\"e"}},
		{"comment line", "# a comment\nFOO=bar\n", map[string]string{"FOO": "bar"}},
		{"blank line", "\nFOO=bar\n\n", map[string]string{"FOO": "bar"}},
		{"trailing comment stripped only outside quotes", "FOO=bar # comment\nBAR='baz # not comment'\n", map[string]string{"FOO": "bar", "BAR": "baz # not comment"}},
		{"empty value", "FOO=\n", map[string]string{"FOO": ""}},
		{"whitespace around =", "FOO = bar\n", map[string]string{"FOO": "bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.content)
			got, err := envfile.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Load() = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestLoadMalformedKey(t *testing.T) {
	path := writeTemp(t, "1FOO=bar\n")
	_, err := envfile.Load(path)
	if err == nil {
		t.Fatal("Load() = nil error, want error")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("Load() error = %v, want mention of invalid key", err)
	}
}

func TestLoadUnterminatedQuote(t *testing.T) {
	path := writeTemp(t, "FOO=\"bar\n")
	_, err := envfile.Load(path)
	if err == nil {
		t.Fatal("Load() = nil error, want error")
	}
	if !strings.Contains(err.Error(), "unterminated quote") {
		t.Errorf("Load() error = %v, want mention of unterminated quote", err)
	}
}

func TestLoadDuplicateKeyLastWins(t *testing.T) {
	path := writeTemp(t, "FOO=first\nFOO=second\n")
	got, err := envfile.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["FOO"] != "second" {
		t.Errorf("FOO = %q, want second", got["FOO"])
	}
}

func TestLoadLiteralEqualsInValue(t *testing.T) {
	path := writeTemp(t, "KEY=a=b\n")
	got, err := envfile.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["KEY"] != "a=b" {
		t.Errorf("KEY = %q, want a=b", got["KEY"])
	}
}

func TestLoadCRLF(t *testing.T) {
	crlf := "FOO=bar\r\nBAZ=qux\r\n"
	lf := "FOO=bar\nBAZ=qux\n"
	pathCRLF := writeTemp(t, crlf)
	pathLF := writeTemp(t, lf)

	gotCRLF, err := envfile.Load(pathCRLF)
	if err != nil {
		t.Fatalf("Load(CRLF): %v", err)
	}
	gotLF, err := envfile.Load(pathLF)
	if err != nil {
		t.Fatalf("Load(LF): %v", err)
	}
	if len(gotCRLF) != len(gotLF) {
		t.Fatalf("CRLF result = %v, LF result = %v", gotCRLF, gotLF)
	}
	for k, v := range gotLF {
		if gotCRLF[k] != v {
			t.Errorf("CRLF key %s = %q, want %q", k, gotCRLF[k], v)
		}
	}
}

func TestLoadErrorNeverContainsValue(t *testing.T) {
	sensitive := "s3cr3t-token"
	tests := []string{
		"1FOO=" + sensitive + "\n",
		"FOO=\"" + sensitive + "\n",
	}
	for _, content := range tests {
		path := writeTemp(t, content)
		_, err := envfile.Load(path)
		if err == nil {
			t.Fatal("Load() = nil error, want error")
		}
		if strings.Contains(err.Error(), sensitive) {
			t.Errorf("Load() error = %v, must not contain parsed value", err)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := envfile.Load(filepath.Join(t.TempDir(), "missing.env"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load(missing) error = %v, want wrapped os.ErrNotExist", err)
	}
}
