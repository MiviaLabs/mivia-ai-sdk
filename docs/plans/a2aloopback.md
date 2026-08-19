# Plan: a2aloopback

Status: planned, not yet built. Extracts `a2aclient/loopback.go` into
its own package. See `docs/plans/a2aclient.md`'s "Loopback extraction"
addendum for the reasoning.

## Goal

Ship a real gRPC A2A server as a test fixture, in one function,
`Loopback`. It lets `a2aclient`'s own tests and other packages'
cross-package tests run a full sign-send-poll-verify round trip
against a live server, with no live remote agent and no mock
transport. It keeps the a2a-go server-side packages out of
`a2aclient`'s own compiled build.

## Scope

Inside: `Loopback`, the unexported `loopbackExecutor` type and its
`Execute` and `Cancel` methods, and the unexported `loopbackPayload`
and `dataFromRaw` helpers. This is `a2aclient/loopback.go`, moved
verbatim into package `a2aloopback`, plus a private copy of
`dataFromRaw` (`a2aclient/grpc.go`'s helper is unexported, so it
cannot be imported; the copy is eight lines and carries no
independent invariant).

Outside: any part of `a2aclient`'s production client surface
(`Client`, `New`, `Close`, `Send`, `Status`, `Result`, `TaskHandle`,
`State`). `a2aloopback` never imports `a2aclient`'s production code.
Its own tests import `a2aclient` to drive a full round trip against
the fixture, the same way `a2aclient/grpc_loopback_integration_test.go`
already does; that import lives in a `_test.go` file only and never
in `a2aloopback/loopback.go` itself.

No production package may import `a2aloopback`. It exists to run
inside another package's own test files or nested `_test` directory,
the same convention `durablefence` already uses in this module (see
`durablefence/doc.go`). `a2aloopback/doc.go` states this rule; no gate
enforces it mechanically, the same gap `durablefence` already accepts,
because `scripts/check_deps.py` exempts every `_test.go` file and
never scans a nested `_test` directory.

## API

```go
package a2aloopback

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"google.golang.org/grpc"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Loopback starts a gRPC A2A server on a 127.0.0.1 loopback port. It
// returns the address and a stop function. Each received task
// completes promptly. No production package may import this package;
// see the package doc comment.
func Loopback() (addr string, stop func() error, err error)
```

`loopbackExecutor`, `loopbackExecutor.Execute`,
`loopbackExecutor.Cancel`, `loopbackPayload`, and `dataFromRaw` stay
unexported, moved without behavior change from
`a2aclient/loopback.go`. `Loopback` is the only exported symbol.

`api/a2aloopback.txt` gains one entry, `func Loopback(...)`, via
`make api-update`. `api/a2aclient.txt` loses its `Loopback`,
`loopbackExecutor.Cancel`, and `loopbackExecutor.Execute` lines in the
same change; see the a2aclient addendum for the full before/after.

## Tests

`a2aloopback` needs its own coverage: the Makefile's coverage block
measures each package in isolation, so `a2aclient`'s existing
round-trip test no longer counts toward `a2aloopback`'s floor once the
code moves.

`a2aloopback/loopback_test.go`, `package a2aloopback` (internal,
whitebox, mirrors `a2aclient/grpc_loopback_integration_test.go`):

- `TestLoopbackRoundTrip` starts `Loopback`, builds an
  `a2aclient.Client` against its address, signs and sends one message,
  polls `Status` until `StateCompleted`, calls `Result`, and asserts
  `VerifySignature` succeeds and the payload round-trips. This
  exercises `Loopback`, `loopbackExecutor.Execute`, and
  `loopbackPayload`'s success path.
- `TestLoopbackExecutorCancel` builds a `loopbackExecutor` directly
  and calls `Cancel` with a stub `eventqueue.Queue`, asserting a
  canceled, final status event is written. `Loopback`'s own server
  flow never reaches `Cancel`; this is the only way to cover it.
- `TestLoopbackStopIsIdempotent` calls `stop` twice and asserts both
  calls return nil, mirroring `a2aclient.Client.Close`'s idempotency
  contract this fixture's caller relies on.

`loopbackPayload`'s two error branches (no message, no payload) stay
uncovered by design, the same reasoning `docs/plans/a2aclient.md`'s
gap-fix section already gives: no real caller can construct a request
missing either field, since `a2a.ToPart` always produces both. This
plan carries that reasoning forward unchanged; it does not reopen it.

## Verification

`make verify` passes for `a2aloopback`: gofmt, vet, the python gates,
the Semgrep scan, and the coverage floor at 85 for `a2aloopback`.
`make api-update` locks `Loopback` into `api/a2aloopback.txt`.

`policy/layers.json` gains `"a2aloopback": ["a2a", "envelope"]`. The
row grants exactly two edges; `a2aloopback` may import nothing else
inside this module. `python3 scripts/check_deps.py` passes.

### Semgrep: a new scoped third-party exception

`sdk.go.stdlib-only-imports`'s `paths.exclude` list
(`semgrep/sdk-standards.yml`) gains `"/a2aloopback/*.go"`, alongside
the existing `a2aclient`, `mcp`, `ledger`, and `schema` entries.

A new rule, `sdk.go.a2aloopback-scoped-third-party-import`, severity
`ERROR`, `paths.include: ["/a2aloopback/*.go"]`, reuses the same
dotted-domain `pattern-regex` and adds three `pattern-not-regex`
lines, matching `sdk.go.a2aclient-scoped-third-party-import`'s actual
three-line shape exactly (`semgrep/sdk-standards.yml:57-67`): first
`(?i)"github\.com/MiviaLabs/mivia-ai-sdk(/[^"\n]*)?"`, excluding this
module's own internal imports, such as `a2aloopback`'s own `a2a` and
`envelope`; then `"github\.com/a2aproject/a2a-go(/[^"\n]*)?"`; then
`"google\.golang\.org/grpc(/[^"\n]*)?"`. Omitting the first line would
make the rule fire ERROR on `a2aloopback/loopback.go`'s own required
`a2a` and `envelope` imports, breaking `make verify` for the package
this plan ships.

`a2aclient`'s own two scoped rules
(`sdk.go.a2aclient-scoped-third-party-import`,
`sdk.go.marshal-via-encode`'s `/a2aclient/grpc.go` exclusion) do not
change: `a2aclient/grpc.go` and `a2aclient/client.go` keep their
existing `a2a-go` and `grpc` imports untouched by this move.

`scripts/check_semgrep_probes.py` gains a probe case for the new rule,
parallel to the existing `a2aclient` case. The probe script's
`expected`/`hits` dicts key by basename alone, pooled across every
scoped-rule fixture directory in one Semgrep run (see the script's own
comment at `scripts/check_semgrep_probes.py:265-267`). Reusing
`a2aclient`'s basenames (`viol_other_import.go`, `clean_a2a_import.go`)
would silently overwrite that probe's `expected` entry instead of
adding a second one, the same reason the `mcp`, `ledger`, and `schema`
probes already use prefixed basenames. This plan's fixtures use
basenames distinct from every basename already in
`scripts/check_semgrep_probes.py`:

- `a2aloopback/viol_a2aloopback_other_import.go` imports an unrelated
  third-party path; `a2aloopback/clean_a2a_go_srv_import.go` imports
  `github.com/a2aproject/a2a-go/a2asrv`;
  `a2aloopback/clean_a2aloopback_internal_import.go` imports
  `github.com/MiviaLabs/mivia-ai-sdk/envelope`, exercising the module-
  path exclusion the omitted third `pattern-not-regex` line would have
  missed.
- Register all three basenames in `expected` against
  `sdk.go.a2aloopback-scoped-third-party-import`.
- Assert `sdk.go.a2aloopback-scoped-third-party-import` fires on
  `viol_a2aloopback_other_import.go` and not on
  `clean_a2a_go_srv_import.go` or `clean_a2aloopback_internal_import.go`;
  assert `sdk.go.stdlib-only-imports` fires on none of the three.

### Semgrep: reject a production import of a2aloopback

`policy/layers.json` and `scripts/check_deps.py` stop a package from
importing `a2aloopback` unless its own row lists the edge, but nothing
stops a production package from adding that row to its own
`policy/layers.json` entry and importing `a2aloopback` in a non-test
file. That import would drag the a2a-go server-side packages and grpc
into a real binary, the exact weight this extraction removes from
`a2aclient`. `durablefence` carries the same convention-only gap, but
`durablefence` adds no third-party import, so an accidental import
there costs nothing. `a2aloopback` carries real third-party weight, so
the same gap costs more here. This plan adds a Semgrep rule to close
it.

A new rule, `sdk.go.no-a2aloopback-import`, severity `ERROR`,
`paths.include: ["/**/*.go"]`, rejects the `a2aloopback` import string
from every file except a named allowlist of legitimate callers:

```yaml
- id: sdk.go.no-a2aloopback-import
  languages: [go]
  severity: ERROR
  message: only a2aloopback's own files, a2aclient's test files, and a2aack's loopback test package may import a2aloopback; no production file may (see AGENTS.md).
  paths:
    include: ["/**/*.go"]
    exclude:
      - "/a2aloopback/*.go"
      - "/a2aclient/*_test.go"
      - "/a2aack/a2aack_test/*.go"
  patterns:
    - pattern-regex: '"github\.com/MiviaLabs/mivia-ai-sdk/a2aloopback(/[^"\n]*)?"'
```

This mirrors `sdk.go.a2aclient-scoped-third-party-import`'s shape,
inverted: that rule rejects every import from one package except an
allowed set of third-party paths; this rule rejects one internal
import path from everywhere except an allowed set of caller paths.

`scripts/check_semgrep_probes.py` gains a second new probe pair for
this rule, with basenames distinct from every existing basename,
including the `a2aloopback/viol_a2aloopback_other_import.go` and
`a2aloopback/clean_a2a_go_srv_import.go` pair added above:

- `viol_a2aloopback_prod_import.go`, written at the probe root (no
  exclude applies there), imports
  `"github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"` from an ordinary
  production-looking package.
- `a2aclient/clean_a2aloopback_caller_import_test.go`, written inside
  the same `a2aclient/` probe fixture directory the
  `a2aclient`-scoped case above already creates, named to match the
  `/a2aclient/*_test.go` exclude, imports the same path.
- Register both basenames in `expected` against
  `sdk.go.no-a2aloopback-import`.
- Assert `sdk.go.no-a2aloopback-import` fires on
  `viol_a2aloopback_prod_import.go` and not on
  `clean_a2aloopback_caller_import_test.go`.

`make verify` for `a2aloopback` passes only once both new rules land:
`sdk.go.a2aloopback-scoped-third-party-import` and
`sdk.go.no-a2aloopback-import`, each with its own probe pair, run
through `scripts/check_semgrep_probes.py` in the same Semgrep scan.

### go.mod and go.sum

No new `require` line is expected: `a2aclient` already requires the
whole `a2aproject/a2a-go` module, and its server-side subpackages
(`a2asrv`, `a2agrpc`, `a2asrv/eventqueue`) ship inside that same
module. The builder runs `go mod tidy` after the move and confirms
`go.mod` and `go.sum` are unchanged, or, if the module graph shifts,
reconciles `scripts/check_gomod.py`'s `ALLOWED_MODULES` the same way
`docs/plans/a2aclient.md`'s go.mod section already describes: trim or
add entries to match `go mod tidy`'s real output, never widen the set
beyond what tidy actually adds.

### AGENTS.md: the relocated and split exception

See `docs/plans/a2aclient.md`'s addendum for the exact sentence and
layout-bullet edits this plan needs in `AGENTS.md`, applied by the
builder in the same change as the code, per that file's own
write-scope rule.
