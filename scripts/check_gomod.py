#!/usr/bin/env python3
"""Gate: go.mod and go.sum carry no dependency outside a named, closed
allowlist. The SDK is standard library only, with one deliberate
exception: a2aclient wraps github.com/a2aproject/a2a-go (see
docs/plans/a2aclient.md). ALLOWED_MODULES is that module's verified
dependency closure, reconciled against real `go mod tidy` output; a
require or go.sum line for any other module path fails the gate.
`replace`, `exclude`, and `retract` directives stay fully rejected, no
exception."""
import re
import sys
from pathlib import Path

BLOCKED_DIRECTIVES = ("replace", "exclude", "retract")
DIRECTIVE = re.compile(r"^(require|replace|exclude|retract)\b")
REQUIRE_LINE = re.compile(r"^([A-Za-z0-9._/-]+)\s+v")

# The module a2aclient wraps, plus its resolved dependency closure. Kept
# in sync with go.mod's require block and go.sum's module set by
# `go mod tidy`; trim an entry when a future tidy stops adding it.
ALLOWED_MODULES = {
    "github.com/a2aproject/a2a-go",
    "github.com/go-logr/logr",
    "github.com/go-logr/stdr",
    "github.com/golang/protobuf",
    "github.com/google/go-cmp",
    "github.com/google/uuid",
    "go.opentelemetry.io/auto/sdk",
    "go.opentelemetry.io/otel",
    "go.opentelemetry.io/otel/metric",
    "go.opentelemetry.io/otel/sdk",
    "go.opentelemetry.io/otel/sdk/metric",
    "go.opentelemetry.io/otel/trace",
    "golang.org/x/net",
    "golang.org/x/sync",
    "golang.org/x/sys",
    "golang.org/x/text",
    "google.golang.org/genproto/googleapis/api",
    "google.golang.org/genproto/googleapis/rpc",
    "google.golang.org/grpc",
    "google.golang.org/protobuf",
}


def check_gomod(gomod: Path) -> list[str]:
    problems = []
    in_require = False
    depth = 0
    for n, line in enumerate(gomod.read_text().splitlines(), 1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if not in_require:
            m = DIRECTIVE.match(stripped)
            if m:
                directive = m.group(1)
                if directive in BLOCKED_DIRECTIVES:
                    problems.append(f"go.mod:{n}: directive {directive} is forbidden; standard library only")
                    continue
                if directive == "require" and stripped.endswith("("):
                    in_require = True
                    continue
                if directive == "require":
                    problems.extend(check_require_line(stripped[len("require"):].strip(), n))
            continue
        if stripped == ")":
            in_require = False
            continue
        problems.extend(check_require_line(stripped, n))
    return problems


def check_require_line(text: str, n: int) -> list[str]:
    m = REQUIRE_LINE.match(text)
    if not m:
        return []
    path = m.group(1)
    if path not in ALLOWED_MODULES:
        return [f"go.mod:{n}: require {path} is outside ALLOWED_MODULES"]
    return []


def check_gosum(gosum: Path) -> list[str]:
    if not gosum.exists():
        return []
    problems = []
    for n, line in enumerate(gosum.read_text().splitlines(), 1):
        stripped = line.strip()
        if not stripped:
            continue
        path = stripped.split()[0]
        if path not in ALLOWED_MODULES:
            problems.append(f"go.sum:{n}: module {path} is outside ALLOWED_MODULES")
    return problems


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    gomod = root / "go.mod"
    if not gomod.exists():
        print("go.mod: missing")
        return 1
    problems = check_gomod(gomod) + check_gosum(root / "go.sum")
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
