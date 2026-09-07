package envfile_test

import (
	"os"
	"path/filepath"

	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
	"github.com/MiviaLabs/mivia-ai-sdk/x/envfile"
)

// TestWorkspaceEnvfileComposedPath runs the composed secret path end
// to end: write a permitted dotenv through a Deny-guarded workspace,
// read it back, parse the bytes with envfile.LoadBytes, then confirm a
// denied path refuses and leaks no parsed value.
func TestWorkspaceEnvfileComposedPath(t *testing.T) {
	const (
		body        = "# app config\nAPI_KEY=k3y-from-app-env\nENDPOINT='https://example.test/v1'\n"
		wantKey     = "k3y-from-app-env"
		deniedValue = "prod-secret-value"
	)
	root := t.TempDir()
	envWriteUnderTest(t, root, "secrets/prod.env", "API_KEY="+deniedValue+"\n")
	w := envOpenDeny(t, root, []string{"secrets/"})

	if err := w.WriteFile("config/app.env", []byte(body)); err != nil {
		t.Fatalf("WriteFile(config/app.env): %v", err)
	}
	data, err := w.ReadFile("config/app.env")
	if err != nil {
		t.Fatalf("ReadFile(config/app.env): %v", err)
	}

	values, err := envfile.LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if values["API_KEY"] != wantKey {
		t.Errorf("API_KEY = %q, want %q", values["API_KEY"], wantKey)
	}
	if values["ENDPOINT"] != "https://example.test/v1" {
		t.Errorf("ENDPOINT = %q, want https://example.test/v1", values["ENDPOINT"])
	}

	denied, err := w.ReadFile("secrets/prod.env")
	if !errors.Is(err, workspace.ErrSecretPath) {
		t.Fatalf("ReadFile(secrets/prod.env) error = %v, want ErrSecretPath", err)
	}
	if denied != nil {
		t.Errorf("ReadFile(secrets/prod.env) = %q, want nil bytes", denied)
	}
	for _, value := range []string{wantKey, deniedValue, "https://example.test/v1"} {
		if strings.Contains(err.Error(), value) {
			t.Errorf("denial error = %v, must not contain the value %q", err, value)
		}
	}
}

// envMatcher compiles patterns into a workspace matcher.
func envMatcher(t *testing.T, patterns []string) *workspace.Matcher {
	t.Helper()
	m, err := workspace.NewMatcher(patterns)
	if err != nil {
		t.Fatalf("NewMatcher(%v): %v", patterns, err)
	}
	return m
}

// envOpenDeny opens a Workspace on root under a Deny matcher compiled
// from patterns.
func envOpenDeny(t *testing.T, root string, patterns []string) *workspace.Workspace {
	t.Helper()
	opts := workspace.Options{Root: root, Deny: envMatcher(t, patterns)}
	w, err := workspace.OpenWith(opts)
	if err != nil {
		t.Fatalf("OpenWith(%q): %v", root, err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// envWriteUnderTest writes a fixture file outside the package API, so
// no deny rule can change the fixture.
func envWriteUnderTest(t *testing.T, root, name, data string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", full, err)
	}
}
