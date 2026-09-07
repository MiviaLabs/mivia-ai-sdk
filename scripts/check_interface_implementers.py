#!/usr/bin/env python3
"""Advisory: flag an interface with exactly one production implementer
and no real consumer - the premature-interface-extraction shape this
repo's architecture-review series found four times before anyone
built a gate for it (provider.ContextAccountant,
provider.ReasoningPolicy, tools.ResultBudgetTool,
tools.PrivilegedTool, each shipped with one implementer and zero
consumers for a stretch). staticcheck's U1000 stopped flagging unused
exported identifiers in 2020.2 to avoid public-API false positives,
and no golangci-lint linter covers single-implementer interfaces, so
this gate exists to fill that gap.

Method, and its limits: every interface is read from api/<pkg>.txt
(the locked surface, so this gate never needs its own Go parser). Its
method set is then matched, by name only, against `func (recv
Type) Method(` definitions in the module's non-test .go files - and,
when named via --sibling or the SDK_CONSUMER_PATH environment
variable, an external consumer checkout. This is a name-text search,
not a type-checked structural match: it does not check parameter or
return types, so two unrelated interfaces that happen to share a
method name and nothing else can both read a stray type as a
"producer". It also cannot see an interface assembled by embedding
another interface (a method contributed through an embedded interface
name, not written with its own parentheses in the lock, is invisible
to this scan). Both gaps undercount evidence in the same direction as
check_symbol_wiring.py's name search: a false "earned" verdict is
more likely than a false "single-implementer" one, so review the
report before trusting a low producer count.

A consumer is counted the same way: any non-test occurrence of the
interface's own name beyond the line that declares it - a parameter,
a return type, a type assertion, or a doc comment happening to name
it. The last case is the same over-count risk check_symbol_wiring.py
already accepts for its symbol search.

This gate is ADVISORY ONLY. A public SDK legitimately ships some
single-implementer interfaces as an intentional extension point -
`provider.Completer` has one production adapter today
(provider/anthropic) by design, not by accident - so this check never
hard-fails `make verify` or `make verify-fast`. It always exits 0
unless the caller passes --strict, which is for manual or
CI-informational use only."""
import argparse
import os
import re
import sys
import tempfile
from pathlib import Path

_INTERFACE_RE = re.compile(r"^  type (\w+) interface \{$")
_METHOD_LINE_RE = re.compile(r"^\t(\w+)\(")
_METHOD_DEF_RE = re.compile(r"^func\s*\(\s*(?:\w+\s+)?\*?(\w+)\)\s+(\w+)\(")
_LINE_COMMENT = re.compile(r"//.*$")

VERDICT_EARNED = "earned"
VERDICT_SINGLE = "single-implementer"
VERDICT_UNIMPLEMENTED = "unimplemented"


def parse_interfaces(text: str) -> dict[str, list[str]]:
    """parse_interfaces extracts every `type X interface { ... }` block
    from one api/<pkg>.txt lock, returning interface name to its
    method-name list. A `type X struct { ... }` block, and any other
    brace body, is walked and discarded the same way
    check_symbol_wiring.py's parse_symbols discards struct fields: a
    field or a non-interface body never sets a current interface name,
    so nothing inside it is recorded."""
    interfaces: dict[str, list[str]] = {}
    depth = 0
    current_name: str | None = None
    current_methods: list[str] = []
    for line in text.splitlines():
        if depth > 0:
            if line == "}":
                if current_name is not None and current_methods:
                    interfaces[current_name] = current_methods
                depth = 0
                current_name = None
                current_methods = []
                continue
            m = _METHOD_LINE_RE.match(line)
            if m:
                current_methods.append(m.group(1))
            continue
        m = _INTERFACE_RE.match(line)
        if m:
            current_name = m.group(1)
            current_methods = []
            depth = 1
            continue
        if line.endswith("{"):
            depth = 1
    return interfaces


def iter_lock_files(root: Path):
    """iter_lock_files yields every api/*.txt lock, package path
    first, then the file. Mirrors check_symbol_wiring.py."""
    api_dir = root / "api"
    if not api_dir.is_dir():
        return
    for path in sorted(api_dir.rglob("*.txt")):
        pkg = str(path.relative_to(api_dir))[: -len(".txt")]
        yield pkg, path


def _iter_go_files(root: Path):
    """_iter_go_files walks non-test .go files under root, matching
    check_symbol_wiring.py's exclusions: dotfiles, docs/ outside
    examples, and _test.go files."""
    for path in root.rglob("*.go"):
        rel = path.relative_to(root)
        parts = rel.parts
        if any(p.startswith(".") for p in parts):
            continue
        if parts and parts[0] == "docs" and "examples" not in parts:
            continue
        if path.name.endswith("_test.go"):
            continue
        yield path


def _strip_line_comments(text: str) -> str:
    """_strip_line_comments removes a trailing "//" comment from every
    line, so a doc comment naming the interface does not read as a
    real consumer any more reliably than check_symbol_wiring.py's own
    stripping does for a plain symbol name."""
    return "\n".join(_LINE_COMMENT.sub("", line) for line in text.splitlines())


def collect_type_methods(files) -> dict[str, set[str]]:
    """collect_type_methods scans `func (recv [*]Type) Method(` lines
    across the given files and returns each concrete type name mapped
    to the method names defined on it. Name-only match: no parameter
    or return-type check, so this is a heuristic, not a structural
    proof of interface satisfaction."""
    type_methods: dict[str, set[str]] = {}
    for path in files:
        try:
            text = path.read_text(errors="ignore")
        except OSError:
            continue
        for raw_line in _strip_line_comments(text).splitlines():
            m = _METHOD_DEF_RE.match(raw_line.strip())
            if m:
                type_methods.setdefault(m.group(1), set()).add(m.group(2))
    return type_methods


def producers_for(methods: list[str], type_methods: dict[str, set[str]]) -> list[str]:
    """producers_for returns the sorted concrete type names whose
    method set is a superset of the interface's method names."""
    required = set(methods)
    return sorted(t for t, have in type_methods.items() if required <= have)


def count_consumers(name: str, files) -> int:
    """count_consumers counts non-test occurrences of the interface
    name beyond the line that declares it: a parameter, a return
    type, a type assertion, a var declaration, or (over-counting, on
    the same precision-first tradeoff check_symbol_wiring.py accepts)
    a doc comment that happens to name it."""
    pattern = re.compile(r"\b" + re.escape(name) + r"\b")
    decl_pattern = re.compile(r"^type\s+" + re.escape(name) + r"\s+interface\b")
    total = 0
    for path in files:
        try:
            text = path.read_text(errors="ignore")
        except OSError:
            continue
        for line in _strip_line_comments(text).splitlines():
            hits = len(pattern.findall(line))
            if hits and decl_pattern.match(line.strip()):
                hits -= 1
            total += hits
    return total


def sibling_repo(root: Path, override: str | None) -> Path | None:
    """sibling_repo resolves an external consumer checkout from
    --sibling, falling back to the SDK_CONSUMER_PATH environment
    variable already used by check_symbol_wiring.py. Neither names a
    specific repo. No path, or a path that does not exist, means the
    check runs module-only."""
    value = override or os.environ.get("SDK_CONSUMER_PATH")
    if not value:
        return None
    candidate = Path(value)
    return candidate if candidate.is_dir() else None


class Finding:
    """Finding is one interface's evidence and verdict."""

    def __init__(self, pkg: str, name: str, producers: list[str], consumers: int):
        self.pkg = pkg
        self.name = name
        self.producers = producers
        self.consumers = consumers

    @property
    def verdict(self) -> str:
        if not self.producers:
            return VERDICT_UNIMPLEMENTED
        if len(self.producers) == 1 and self.consumers == 0:
            return VERDICT_SINGLE
        return VERDICT_EARNED

    def line(self) -> str:
        return (
            f"{self.pkg}.{self.name}: producers={len(self.producers)} "
            f"({', '.join(self.producers) or 'none'}) consumers={self.consumers} "
            f"verdict={self.verdict}"
        )


def analyze(root: Path, sibling: Path | None = None) -> list[Finding]:
    """analyze runs the heuristic against one repo root and returns
    one Finding per interface found in the api/ locks, sorted by
    package then name."""
    module_files = list(_iter_go_files(root))
    sibling_files = list(_iter_go_files(sibling)) if sibling else []
    all_files = module_files + sibling_files
    type_methods = collect_type_methods(all_files)

    findings: list[Finding] = []
    for pkg, lock_path in iter_lock_files(root):
        for name, methods in parse_interfaces(lock_path.read_text()).items():
            producers = producers_for(methods, type_methods)
            consumers = count_consumers(name, all_files)
            findings.append(Finding(pkg, name, producers, consumers))
    findings.sort(key=lambda f: (f.pkg, f.name))
    return findings


def format_report(findings: list[Finding]) -> str:
    if not findings:
        return "no interfaces found in api/*.txt locks"
    return "\n".join(f.line() for f in findings)


# --- probes ---------------------------------------------------------


def _write(root: Path, rel: str, text: str) -> None:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)


def _probe_two_producers_reads_earned(root: Path) -> list[str]:
    _write(root, "api/iface.txt", "package iface\n  type Runner interface {\n\tRun() error\n}\n")
    _write(root, "iface/iface.go", "package iface\n\ntype Runner interface {\n\tRun() error\n}\n")
    _write(root, "producer1/producer1.go", "package producer1\n\ntype A struct{}\n\nfunc (a A) Run() error { return nil }\n")
    _write(root, "producer2/producer2.go", "package producer2\n\ntype B struct{}\n\nfunc (b B) Run() error { return nil }\n")
    findings = analyze(root)
    match = next((f for f in findings if f.name == "Runner"), None)
    if match is None or match.verdict != VERDICT_EARNED or len(match.producers) != 2:
        return [f"probe_two_producers_reads_earned: expected earned/2 producers, got {match.line() if match else None}"]
    return []


def _probe_single_producer_zero_consumers_reads_single_implementer(root: Path) -> list[str]:
    _write(root, "api/iface2.txt", "package iface2\n  type Doer interface {\n\tDo() error\n}\n")
    _write(root, "iface2/iface2.go", "package iface2\n\ntype Doer interface {\n\tDo() error\n}\n")
    _write(root, "implementer/implementer.go", "package implementer\n\ntype Impl struct{}\n\nfunc (i Impl) Do() error { return nil }\n")
    findings = analyze(root)
    match = next((f for f in findings if f.name == "Doer"), None)
    if match is None or match.verdict != VERDICT_SINGLE or match.consumers != 0:
        return [f"probe_single_producer_zero_consumers_reads_single_implementer: expected single-implementer/0 consumers, got {match.line() if match else None}"]
    return []


def _probe_test_only_producer_does_not_count(root: Path) -> list[str]:
    _write(root, "api/iface3.txt", "package iface3\n  type Closer2 interface {\n\tCloseAll() error\n}\n")
    _write(root, "iface3/iface3.go", "package iface3\n\ntype Closer2 interface {\n\tCloseAll() error\n}\n")
    _write(root, "mock/mock_test.go", "package mock\n\ntype MockCloser struct{}\n\nfunc (m MockCloser) CloseAll() error { return nil }\n")
    findings = analyze(root)
    match = next((f for f in findings if f.name == "Closer2"), None)
    if match is None or match.producers or match.verdict != VERDICT_UNIMPLEMENTED:
        return [f"probe_test_only_producer_does_not_count: expected unimplemented/0 producers, got {match.line() if match else None}"]
    return []


def _probe_struct_fields_excluded_from_interface_methods(root: Path) -> list[str]:
    _write(root, "api/iface4.txt", (
        "package iface4\n"
        "  type Config struct {\n"
        "  Timeout int\n"
        "}\n"
        "  type Runner4 interface {\n"
        "\tRun() error\n"
        "}\n"
    ))
    text = (root / "api/iface4.txt").read_text()
    interfaces = parse_interfaces(text)
    if "Config" in interfaces:
        return [f"probe_struct_fields_excluded_from_interface_methods: struct wrongly read as an interface, got {interfaces}"]
    if interfaces.get("Runner4") != ["Run"]:
        return [f"probe_struct_fields_excluded_from_interface_methods: expected Runner4:['Run'], got {interfaces.get('Runner4')}"]
    return []


def _probe_unnamed_receiver_keeps_type_name(root: Path) -> list[str]:
    _write(root, "api/iface5.txt", "package iface5\n  type Walker interface {\n\tWalk() error\n}\n")
    _write(root, "walker/walker.go", "package walker\n\ntype Step struct{}\n\nfunc (Step) Walk() error { return nil }\n")
    findings = analyze(root)
    match = next((f for f in findings if f.name == "Walker"), None)
    if match is None or match.verdict != VERDICT_SINGLE or match.producers != ["Step"]:
        return [f"probe_unnamed_receiver_keeps_type_name: expected single-implementer/['Step'], got {match.line() if match else None}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the gate's own logic against small,
    isolated temp-directory fixtures, following
    check_symbol_wiring.py's --probe convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="interface-implementers-probe-") as tmp:
        for fn in (
            _probe_two_producers_reads_earned,
            _probe_single_producer_zero_consumers_reads_single_implementer,
            _probe_test_only_producer_does_not_count,
            _probe_struct_fields_excluded_from_interface_methods,
            _probe_unnamed_receiver_keeps_type_name,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))

    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="interface-implementers advisory scan")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    parser.add_argument("--sibling", help="path to an external consumer repo checkout")
    parser.add_argument(
        "--strict", action="store_true",
        help="exit 1 when any interface reads single-implementer or unimplemented "
             "(manual/CI-informational use only; never wired into verify or verify-fast)",
    )
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    sibling = sibling_repo(root, args.sibling)
    findings = analyze(root, sibling)
    print(format_report(findings))

    if args.strict:
        flagged = [f for f in findings if f.verdict != VERDICT_EARNED]
        if flagged:
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
