package anthropic_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

type serverHandlerFunc func(w http.ResponseWriter, r *http.Request)

type testClientFixture struct {
	client  *anthropic.Client
	baseURL string
	httpCli *http.Client
}

func newTestServer(t *testing.T, handler serverHandlerFunc) (*httptest.Server, *testClientFixture) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(ts.Close)

	client, err := anthropic.New(anthropic.Options{
		APIKey:     "test-key",
		BaseURL:    ts.URL,
		HTTPClient: ts.Client(),
	})
	if err != nil {
		t.Fatalf("anthropic.New: %v", err)
	}
	return ts, &testClientFixture{
		client:  client,
		baseURL: ts.URL,
		httpCli: ts.Client(),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
