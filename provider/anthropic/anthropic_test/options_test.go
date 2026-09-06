package anthropic_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

type optionsTestCase struct {
	name    string
	opts    anthropic.Options
	wantErr error
}

func optionsTestCases() []optionsTestCase {
	return []optionsTestCase{
		{
			name:    "missing api key",
			opts:    anthropic.Options{},
			wantErr: anthropic.ErrAPIKeyRequired,
		},
		{
			name: "valid options",
			opts: anthropic.Options{APIKey: "sk-ant-test", BaseURL: "https://api.anthropic.com"},
		},
		{
			name:    "invalid base url",
			opts:    anthropic.Options{APIKey: "sk-ant-test", BaseURL: "://bad-url"},
			wantErr: anthropic.ErrInvalidOptions,
		},
		{
			name:    "relative base url",
			opts:    anthropic.Options{APIKey: "sk-ant-test", BaseURL: "/relative/path"},
			wantErr: anthropic.ErrInvalidOptions,
		},
		{
			name:    "negative max tokens non-streaming",
			opts:    anthropic.Options{APIKey: "sk-ant-test", MaxTokensNonStreaming: -1},
			wantErr: anthropic.ErrInvalidOptions,
		},
		{
			name:    "negative max tokens streaming",
			opts:    anthropic.Options{APIKey: "sk-ant-test", MaxTokensStreaming: -1},
			wantErr: anthropic.ErrInvalidOptions,
		},
		{
			name:    "negative max retries",
			opts:    anthropic.Options{APIKey: "sk-ant-test", MaxRetries: -1},
			wantErr: anthropic.ErrInvalidOptions,
		},
		{
			name:    "negative context window",
			opts:    anthropic.Options{APIKey: "sk-ant-test", ContextWindow: -1},
			wantErr: anthropic.ErrInvalidOptions,
		},
	}
}

// TestNewDefaultsContextWindow pins that a zero Options.ContextWindow
// is not left at zero: New derives it from Model (or DefaultModel when
// Model is empty), and a caller-set value always wins.
func TestNewDefaultsContextWindow(t *testing.T) {
	cases := []struct {
		name string
		opts anthropic.Options
		want int
	}{
		{"default model", anthropic.Options{APIKey: "k"}, 1000000},
		{"named current model", anthropic.Options{APIKey: "k", Model: "claude-haiku-4-5"}, 200000},
		{"unrecognized model falls back", anthropic.Options{APIKey: "k", Model: "some-future-model"}, 200000},
		{"caller value wins", anthropic.Options{APIKey: "k", ContextWindow: 42}, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := anthropic.New(tc.opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := c.ContextWindow(); got != tc.want {
				t.Errorf("ContextWindow() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestOptionsValidate(t *testing.T) {
	for _, tc := range optionsTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				c, err := anthropic.New(tc.opts)
				if err != nil {
					t.Fatalf("New failed: %v", err)
				}
				if c.Name() != "anthropic" {
					t.Fatalf("Name() = %q, want 'anthropic'", c.Name())
				}
			} else {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Validate error = %v, want errors.Is %v", err, tc.wantErr)
				}
			}
		})
	}
}
