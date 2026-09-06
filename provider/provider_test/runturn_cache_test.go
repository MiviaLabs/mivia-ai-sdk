package provider_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestRunTurnPreservesCacheUsageAndWebSearch pins the response-side
// pass-through contract: a Completer-reported CacheUsage, one row per
// CacheStyle constant, and a WebSearchResult set reach Response
// unmodified on both the Chat and the ChatStream paths. The
// request-side controls live in request_forwarding_test.go.
func TestRunTurnPreservesCacheUsageAndWebSearch(t *testing.T) {
	webSearch := []provider.WebSearchResult{
		{Title: "first", Content: "alpha", Link: "https://one.example", PublishDate: "2026-01-01"},
		{Title: "second", Content: "beta", Link: "https://two.example", Media: "image"},
	}
	cases := []struct {
		name       string
		stream     bool
		cacheUsage provider.CacheUsage
	}{
		{
			name:       "none style chat",
			stream:     false,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleNone, InputTokens: 10, CachedInputTokens: 4},
		},
		{
			name:       "none style stream",
			stream:     true,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleNone, InputTokens: 11, CacheWriteTokens: 5},
		},
		{
			name:       "implicit style chat",
			stream:     false,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleImplicit, InputTokens: 20, CachedInputTokens: 8},
		},
		{
			name:       "implicit style stream",
			stream:     true,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleImplicit, InputTokens: 21, CacheWriteTokens: 6},
		},
		{
			name:       "explicit style chat",
			stream:     false,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleExplicit, InputTokens: 30, CachedInputTokens: 12, CacheWriteTokens: 7},
		},
		{
			name:       "explicit style stream",
			stream:     true,
			cacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleExplicit, InputTokens: 31, CachedInputTokens: 13, CacheWriteTokens: 8},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got provider.Response
			var err error
			if tc.stream {
				f := &fakeCompleter{name: "fake", streamChunks: []provider.Chunk{
					{Delta: "partial"},
					{Done: true, FinishReason: "stop", CacheUsage: tc.cacheUsage, WebSearch: webSearch},
				}}
				got, err = provider.RunTurn(context.Background(), f, provider.Request{Stream: true})
			} else {
				f := &fakeCompleter{name: "fake", chatResp: provider.Response{
					Message:    provider.Message{Role: provider.RoleAssistant, Content: "done"},
					CacheUsage: tc.cacheUsage,
					WebSearch:  webSearch,
				}}
				got, err = provider.RunTurn(context.Background(), f, provider.Request{Stream: false})
			}
			if err != nil {
				t.Fatalf("RunTurn() error = %v, want nil", err)
			}
			if got.CacheUsage != tc.cacheUsage {
				t.Fatalf("CacheUsage = %+v, want the Completer's %+v unmodified", got.CacheUsage, tc.cacheUsage)
			}
			if !reflect.DeepEqual(got.WebSearch, webSearch) {
				t.Fatalf("WebSearch = %+v, want the Completer's %+v unmodified", got.WebSearch, webSearch)
			}
		})
	}
}
