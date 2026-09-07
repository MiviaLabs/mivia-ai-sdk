package main

import (
	"crypto/tls"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
)

// a2aSurface builds a TLS client the way a remote link does. The
// transport opens lazily; Close releases it before the surface
// returns.
func a2aSurface() error {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	client, err := a2a.NewWithTLS("https://127.0.0.1:1", cfg)
	if err != nil {
		return fmt.Errorf("NewWithTLS: %w", err)
	}
	if err := client.Close(); err != nil {
		return fmt.Errorf("client Close: %w", err)
	}
	return nil
}
