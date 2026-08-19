package workspace_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envfile"
	"github.com/MiviaLabs/mivia-ai-sdk/workspace"
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
	writeUnderTest(t, root, "secrets/prod.env", "API_KEY="+deniedValue+"\n")
	w := openDeny(t, root, []string{"secrets/"})

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
