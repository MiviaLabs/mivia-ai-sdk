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
