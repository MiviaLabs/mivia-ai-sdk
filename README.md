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
- 🛡️ **Confinement & Long-Term Context** — Syscall-level filesystem confinement (`os.Root`), secret path denial, token-window compaction, and content-addressed memory (`workspace`, `contextplan`, `longtermmemory`).

## Install

```bash
go get github.com/MiviaLabs/mivia-ai-sdk
```

## Quick Start

Compose an agent pipeline from an identity, a capability card, a two-step plan, and registered tools. A single `agentrun.Options` literal wires and validates the pipeline:

```go
package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

type prefixTool struct {
	name   string
	prefix string
}

func (t prefixTool) Name() string { return t.name }

func (t prefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	s, _ := in.Value.(string)
	return tools.Out{Value: t.prefix + s}, nil
}

func main() {
	artifacts := &agentrun.Artifacts{}
	plan, err := flow.New([]flow.Step{
		{ID: "review", To: "reviewed", Payload: "invoice 42"},
		{ID: "ship", To: "shipped", Needs: []string{"review"},
			PayloadFrom: agentrun.PayloadOf("review", artifacts)},
	}, nil)
	if err != nil {
		panic(err)
	}

	id, err := identity.New()
	if err != nil {
		panic(err)
	}
	a, err := agent.New(id, discovery.Card{
		Name: "invoice-agent", Capabilities: []string{"invoice.review"},
	}, plan)
	if err != nil {
		panic(err)
	}

	reg := tools.New()
	_ = reg.Add(prefixTool{name: "review", prefix: "reviewed: "})
	_ = reg.Add(prefixTool{name: "ship", prefix: "shipped: "})

	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "reviewed", Trigger: "run"},
		machine.Transition{From: "reviewed", To: "shipped", Trigger: "run"},
	)
	if err != nil {
		panic(err)
	}

	runner, err := agentrun.New(agentrun.Options{
		Agent: a, Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		panic(err)
	}

	status, _, err := runner.Run(context.Background(), "thread-1", machine.InOut{})
	if err != nil {
		panic(err)
	}

	ship, _ := artifacts.Get("ship")
	fmt.Println("status:", status)      // prints "status: shipped"
	fmt.Println("ship artifact:", ship) // prints "shipped: reviewed: invoice 42"
}
```

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

See [AGENTS.md](AGENTS.md) for architecture rules, package isolation policies, and contribution guidelines.

## Author & Contributors

- **Maciej (Mac) Lisowski** — *Author / Lead Architect* ([@mac-lisowski](https://github.com/mac-lisowski))

Contributions are welcome! See [AGENTS.md](AGENTS.md) for contribution rules.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.

