<p align="center">
  <img src="docs/mivia-logo.png" alt="mivia" width="120">
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

- 🔒 **Verifiable Agent Messaging** — Cryptographic envelopes signed with Ed25519, tamper-evident hash audit chains, and semantic acknowledgments (`envelope`, `room`, `identity`).
- 🔄 **Deterministic Workflows & State** — Declarative step graphs, parallel execution waves, guarded state machines, retries, loops, and pause/resume checkpoints (`flow`, `machine`).
- 🧰 **Extensible Tools & MCP** — Named tool registries, permission scoping, approval gating, and MCP client support over stdio or streamable HTTP (`tools`, `mcp`).
- 🤝 **Interoperability & Protocols** — Native A2A v1.0 mapping, gRPC client adapter, and NDJSON HTTP streaming endpoints (`a2a`, `a2aclient`, `dispatch`).
- 🛡️ **Confinement & Long-Term Context** — Syscall-level filesystem confinement (`os.Root`), secret path denial, token-window compaction, and content-addressed memory (`workspace` and `context/plan`).

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk
```

## Quick Start

Run a model-driven agent loop: one schema tool, one `Run` call. The program needs `ANTHROPIC_API_KEY` in the environment at run time.

```go
package main

import (
	"context"
	"encoding/json"
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

func (upperTool) Name() string { return "upper" }

func (upperTool) ParameterSchema() []byte {
	return []byte(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
}

func (upperTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args.Text}, nil
}

func (upperTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: strings.ToUpper(s)}, nil
}

func main() {
	completer, err := anthropic.New(anthropic.Options{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
	})
	if err != nil {
		panic(err)
	}

	reg := tools.New()
	_ = reg.Add(upperTool{})

	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.DefaultBounds(),
	})
	if err != nil {
		panic(err)
	}

	res, err := loop.Run(context.Background(), []provider.Message{
		{Role: provider.RoleUser, Content: "Uppercase the word hello."},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Final.Content)
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

