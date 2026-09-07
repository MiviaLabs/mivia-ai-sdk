<p align="center">
  <img src="docs/logo.png" alt="mivia-ai-sdk" width="120">
</p>

<h1 align="center">mivia-ai-sdk</h1>

<p align="center">Go SDK for building reliable AI agents and multi-agent workflows. Composable blocks, not a monolith.</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8.svg" alt="Go 1.25+">
  <a href="docs/README.md"><img src="https://img.shields.io/badge/docs-reference-purple.svg" alt="Documentation"></a>
</p>

---

`mivia-ai-sdk` provides a set of independent, composable building blocks for building autonomous agents, multi-agent coordination pipelines, and verifiable message exchanges in Go.

Most packages rely solely on the Go standard library, keeping dependencies minimal, auditable, and fast.

## Highlights

- 🔒 **Verifiable Agent Messaging** — Cryptographic envelopes signed with Ed25519, tamper-evident hash audit chains, and semantic acknowledgments (`envelope`, `room`).
- 🔄 **Deterministic Workflows & State** — Declarative step graphs, parallel execution waves, guarded state machines, retries, loops, and pause/resume checkpoints (`flow`, `machine`).
- 🧰 **Extensible Tools & MCP** — Named tool registries, permission scoping, approval gating, and MCP client support over stdio or streamable HTTP (`tools`, `mcp`).
- 🤝 **Interoperability & Protocols** — Native A2A v1.0 mapping, gRPC client adapter, and NDJSON HTTP streaming endpoints (`a2a`, `dispatch`).
- 🛡️ **Confinement & Long-Term Context** — Syscall-level filesystem confinement (`os.Root`), secret path denial, token-window compaction, and content-addressed memory (`workspace` and `context/plan`).

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk
```

## Quick Start

Run a model-driven agent loop: one schema tool, one `Run` call. With `ANTHROPIC_API_KEY` set, it calls the live model; unset, it runs the same loop offline against a canned completer.

<!-- source: docs/examples/_quickstart/main.go -->
```go
// Command quickstart is the README Quick Start program: one schema
// tool, one agentloop.Run call. It uses the live Anthropic completer
// when ANTHROPIC_API_KEY is set, and a canned offline completer
// otherwise, so go run always prints a fixed final line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// upperTool implements tools.Tool and tools.SchemaTool.
type upperTool struct{}

// Name returns the tool's registry name.
func (upperTool) Name() string { return "upper" }

// ParameterSchema returns a JSON object requiring the string property text.
func (upperTool) ParameterSchema() []byte {
	return []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
}

// DecodeArguments unmarshals the raw arguments into the tool's shape.
func (upperTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args.Text}, nil
}

// Run returns the input string uppercased.
func (upperTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: strings.ToUpper(s)}, nil
}

// cannedCompleter scripts one tool call and one final answer, so the
// Quick Start runs offline when ANTHROPIC_API_KEY is unset.
type cannedCompleter struct {
	responses []provider.Response
	calls     int
}

// Name returns the completer's own label.
func (c *cannedCompleter) Name() string { return "canned" }

// Chat returns one scripted response per call, in order.
func (c *cannedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	if c.calls >= len(c.responses) {
		return provider.Response{}, errors.New("cannedCompleter: no response scripted for this call")
	}
	resp := c.responses[c.calls]
	c.calls++
	return resp, nil
}

// ChatStream is unsupported; the scripted exchange is non-streaming.
func (c *cannedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("cannedCompleter: ChatStream not supported")
}

// EstimateTokens sums the request's message content bytes over four.
func (c *cannedCompleter) EstimateTokens(req provider.Request) (int, error) {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	return n / 4, nil
}

// newCompleter returns the live Anthropic completer when
// ANTHROPIC_API_KEY is set, and an offline canned completer otherwise.
func newCompleter() (provider.Completer, error) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return anthropic.New(anthropic.Options{APIKey: key})
	}
	return &cannedCompleter{responses: []provider.Response{
		{
			Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
				{Index: 0, ID: "call-1", Name: "upper", Arguments: []byte(`{"text":"hello"}`)},
			}},
			ToolCalls: []provider.ToolCall{
				{Index: 0, ID: "call-1", Name: "upper", Arguments: []byte(`{"text":"hello"}`)},
			},
			FinishReason: "tool_calls",
		},
		{
			Message:      provider.Message{Role: provider.RoleAssistant, Content: "HELLO"},
			FinishReason: "stop",
		},
	}}, nil
}

func main() {
	completer, err := newCompleter()
	if err != nil {
		fmt.Println("newCompleter:", err)
		return
	}

	reg := tools.New()
	_ = reg.Add(upperTool{})

	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.DefaultBounds(),
	})
	if err != nil {
		fmt.Println("agentloop.New:", err)
		return
	}

	res, err := loop.Run(context.Background(), []provider.Message{
		{Role: provider.RoleUser, Content: "Uppercase the word hello."},
	})
	if err != nil {
		fmt.Println("run:", err)
		return
	}
	fmt.Println("final:", res.Final.Content)
}
```

Use `agentloop` for model-driven agents; use `workflow` for fixed step graphs. See [docs/examples/workflow-run.md](docs/examples/workflow-run.md) for the flow-pipeline example. This Quick Start no longer shows the flow pipeline itself.

## Documentation

- **[Architecture & Design Reference](docs/architecture.md)** — Module map, wire-format rationale, gate system, and architectural invariants.
- **[Doc Index & Package Reference](docs/README.md)** — Comprehensive index covering all packages and their exported surfaces.
- **[Examples & Walkthroughs](docs/README.md#examples)** — Step-by-step guides for provider completion, dispatch endpoints, workflow loops, and durable tasks.

## Development

```bash
make install-hooks   # once per clone; sets core.hooksPath to .githooks
make verify-fast     # fast tier: fmt, vet, test, gates, semgrep scan
make verify          # full tier: coverage floor, semgrep probes, SQLite tests
```

## Author & Contributors

- **Maciej (Mac) Lisowski** — *Author* ([@mac-lisowski](https://github.com/mac-lisowski))

Contributions are welcome!

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.

