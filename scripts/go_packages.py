#!/usr/bin/env python3
"""Shared package enumeration for the structural gates. `go list` is the
sole enumerator, so a package at any depth is visible. Go decides what a
package is: it drops `_`-prefixed and `.`-prefixed segments, testdata
directories, and tag-excluded files. Each package is keyed by its path
relative to the module root. See docs/plans/nested-package-visibility.md."""
import contextlib
import io
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

MODULE = "github.com/MiviaLabs/mivia-ai-sdk"
# BUILD_TAGS names every build tag this module gates a non-test file
# behind. Enumeration runs one `go list` per configuration, so a file
# behind any other constraint would contribute no imports. _constraints
# fails on such a file instead. Keep this list equal to alwaysBuildTags
# in scripts/api_surface.go.
BUILD_TAGS = ("ledger_sqlite",)
SCRIPTS = "scripts"

# Go's own implicit file constraints: a _GOOS, _GOARCH, or _GOOS_GOARCH
# filename suffix. See https://pkg.go.dev/go/build#hdr-Build_Constraints.
GOOS = frozenset((
    "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
    "ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris",
    "wasip1", "windows", "zos",
))
GOARCH = frozenset((
    "386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe",
    "loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le",
    "mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390",
    "s390x", "sparc", "sparc64", "wasm",
))

_CONSTRAINT_LINE = re.compile(r"^//(?:go:build|\s*\+build)\s+(.*)$")
_CONSTRAINT_TERM = re.compile(r"[A-Za-z_][A-Za-z_0-9.]*")

_CACHE: dict[tuple, dict[str, set[str]]] = {}


def probe_env() -> dict[str, str]:
    """probe_env returns the environment a temp-module fixture needs.
    GOPROXY=off and GOFLAGS=-mod=mod keep `go list` off the network."""
    return {"GOPROXY": "off", "GOFLAGS": "-mod=mod"}


def _decode_stream(text: str) -> list[dict]:
    """_decode_stream reads the concatenated JSON objects `go list -json`
    prints."""
    decoder = json.JSONDecoder()
    entries: list[dict] = []
    i = 0
    while True:
        while i < len(text) and text[i].isspace():
            i += 1
        if i >= len(text):
            return entries
        obj, i = decoder.raw_decode(text, i)
        entries.append(obj)


def _go_list(root: Path, tags: str | None, env_extra: dict | None) -> list[dict]:
    """_go_list runs `go list -json ./...` once. It exits non-zero with the
    go stderr on failure; a toolchain failure is never an empty set."""
    cmd = ["go", "list", "-json"]
    if tags:
        cmd += ["-tags", tags]
    cmd.append("./...")
    env = dict(os.environ)
    env.update(env_extra or {})
    out = subprocess.run(cmd, cwd=root, capture_output=True, text=True, env=env)
    if out.returncode != 0:
        print(f"go list failed in {root}:\n{out.stderr}", file=sys.stderr)
        sys.exit(1)
    return _decode_stream(out.stdout)


def _relative(entry: dict) -> str | None:
    """_relative returns the package path relative to the module root, or
    None when the exclusion set drops the package."""
    root = entry.get("Root")
    directory = entry.get("Dir")
    if not root or not directory:
        return None
    rel = os.path.relpath(directory, root).replace(os.sep, "/")
    if rel == ".":
        return None
    if rel == SCRIPTS or rel.startswith(SCRIPTS + "/"):
        return None
    if rel.endswith("_test"):
        return None
    return rel


def _internal_imports(entry: dict) -> set[str]:
    """_internal_imports maps this module's resolved imports to relative
    paths. TestImports and XTestImports stay out: test files are exempt."""
    prefix = MODULE + "/"
    out = set()
    for imp in entry.get("Imports") or []:
        if imp.startswith(prefix):
            out.add(imp[len(prefix):])
    return out


def _filename_constraint(name: str) -> str:
    """_filename_constraint returns the implicit GOOS/GOARCH constraint a
    filename carries, or the empty string."""
    parts = name[: -len(".go")].split("_")
    if len(parts) >= 3 and parts[-2] in GOOS and parts[-1] in GOARCH:
        return f"{parts[-2]}_{parts[-1]}"
    if len(parts) >= 2 and (parts[-1] in GOOS or parts[-1] in GOARCH):
        return parts[-1]
    return ""


def _file_constraint_problems(path: Path, rel: str) -> list[str]:
    """_file_constraint_problems reports every constraint on one non-test
    file that enumeration cannot reach."""
    problems = []
    implicit = _filename_constraint(path.name)
    if implicit:
        problems.append(
            f"{rel}/{path.name}: filename constrains the file to {implicit}; "
            f"enumeration reads no such configuration, so rename the file"
        )
    for line in path.read_text().splitlines():
        if line.startswith("package "):
            break
        match = _CONSTRAINT_LINE.match(line)
        if not match:
            continue
        for term in _CONSTRAINT_TERM.findall(match.group(1)):
            if term not in BUILD_TAGS:
                problems.append(
                    f"{rel}/{path.name}: build constraint names '{term}', outside BUILD_TAGS; "
                    f"add the tag to scripts/go_packages.py and scripts/api_surface.go, "
                    f"or drop the constraint"
                )
    return problems


def _candidate_dirs(root: Path) -> list[str]:
    """_candidate_dirs lists every directory holding a .go file, minus
    the exclusion set. `go list` omits a directory whose files are all
    constrained, so the scan cannot read its list."""
    out = set()
    base = Path(root)
    for path in base.rglob("*.go"):
        rel = path.parent.relative_to(base).as_posix()
        if rel == "." or any(s.startswith(("_", ".")) or s == "testdata" for s in rel.split("/")):
            continue
        if rel == SCRIPTS or rel.startswith(SCRIPTS + "/") or rel.endswith("_test"):
            continue
        out.add(rel)
    return sorted(out)


def _constraints(root: Path, rels: list[str]) -> list[str]:
    """_constraints scans every non-test .go file of the given
    directories. A constraint outside BUILD_TAGS hides the file's
    imports from every `go list` run, so the scan fails closed."""
    problems: list[str] = []
    for rel in sorted(rels):
        directory = Path(root) / rel
        if not directory.is_dir():
            continue
        for path in sorted(directory.glob("*.go")):
            if path.name.endswith("_test.go"):
                continue
            problems.extend(_file_constraint_problems(path, rel))
    return problems


def packages(root: Path, env_extra: dict | None = None) -> dict[str, set[str]]:
    """packages maps every relative package path to its resolved internal
    imports. It unions one `go list` run per BUILD_TAGS configuration, so
    an edge that an enumerated configuration produces is present. A file
    behind another constraint exits the process; that scan walks the
    tree, so an all-constrained directory cannot hide. The result is
    cached per process."""
    key = (str(Path(root).resolve()), tuple(sorted((env_extra or {}).items())))
    if key in _CACHE:
        return _CACHE[key]
    found: dict[str, set[str]] = {}
    for tags in (None, ",".join(BUILD_TAGS)):
        for entry in _go_list(Path(root), tags, env_extra):
            rel = _relative(entry)
            if rel is None:
                continue
            found.setdefault(rel, set()).update(_internal_imports(entry))
    problems = _constraints(Path(root), _candidate_dirs(Path(root)))
    if problems:
        print("\n".join(problems), file=sys.stderr)
        sys.exit(1)
    _CACHE[key] = found
    return found


def package_paths(root: Path, env_extra: dict | None = None) -> list[str]:
    """package_paths returns the sorted relative package paths."""
    return sorted(packages(root, env_extra))


_STDLIB_CACHE: dict[tuple, frozenset] = {}


def _stdlib_set(root: Path, env_extra: dict | None) -> frozenset:
    """_stdlib_set returns the import paths `go list std` reports,
    cached per environment."""
    key = tuple(sorted((env_extra or {}).items()))
    if key in _STDLIB_CACHE:
        return _STDLIB_CACHE[key]
    env = dict(os.environ)
    env.update(env_extra or {})
    out = subprocess.run(["go", "list", "std"], cwd=root, capture_output=True, text=True, env=env)
    if out.returncode != 0:
        print(f"go list std failed:\n{out.stderr}", file=sys.stderr)
        sys.exit(1)
    result = frozenset(out.stdout.split())
    _STDLIB_CACHE[key] = result
    return result


def _own_relative(entry: dict) -> str | None:
    """_own_relative returns the package path relative to the module
    root, keeping every package `go list` reports: `_test` directories
    and `scripts` stay in, unlike `_relative`."""
    root = entry.get("Root")
    directory = entry.get("Dir")
    if not root or not directory:
        return None
    rel = os.path.relpath(directory, root).replace(os.sep, "/")
    if rel == ".":
        return None
    return rel


def third_party_imports(root: Path, tags: str | None,
                         env_extra: dict | None = None) -> dict[str, set[str]]:
    """third_party_imports maps every relative package path to its
    third-party import paths, for one build configuration. It reads
    Imports, TestImports, and XTestImports, so a test-only import is
    seen. It keeps every package, including one with zero third-party
    imports, so a policy key can be validated against this key set."""
    stdlib = _stdlib_set(root, env_extra)
    prefix = MODULE + "/"
    found: dict[str, set[str]] = {}
    for entry in _go_list(Path(root), tags, env_extra):
        rel = _own_relative(entry)
        if rel is None:
            continue
        imports: set[str] = set()
        for key in ("Imports", "TestImports", "XTestImports"):
            imports.update(entry.get(key) or [])
        third_party = set()
        for imp in imports:
            if imp == "C" or imp == MODULE or imp.startswith(prefix):
                continue
            if imp in stdlib:
                continue
            third_party.add(imp)
        found.setdefault(rel, set()).update(third_party)
    return found


def attributed_go_files(root: Path, tags: str | None,
                         env_extra: dict | None = None) -> set[Path]:
    """attributed_go_files returns the resolved absolute paths of every
    file one `go list` pass claims: GoFiles, CgoFiles, TestGoFiles, and
    XTestGoFiles. The residual scan subtracts this set from a full
    tree walk."""
    out: set[Path] = set()
    for entry in _go_list(Path(root), tags, env_extra):
        directory = entry.get("Dir")
        if not directory:
            continue
        for key in ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles"):
            for name in entry.get(key) or []:
                out.add((Path(directory) / name).resolve())
    return out


# --- probes ---------------------------------------------------------


def write_file(root: Path, rel: str, text: str) -> None:
    """write_file writes one probe-fixture file, creating its parents."""
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)


def _write_enumeration_fixture(root: Path) -> None:
    """_write_enumeration_fixture builds one module covering every
    exclusion rule and every import-shape rule."""
    write_file(root, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    write_file(root, "leaf/leaf.go", "package leaf\n\nvar Leaf = 1\n")
    write_file(root, "flow/flow.go", (
        "package flow\n\n"
        f'import "{MODULE}/leaf"\n\n'
        f"// A comment naming {MODULE}/commented is not an import.\n"
        f'var Path = "{MODULE}/stringy"\n\n'
        "var _ = leaf.Leaf\n"
    ))
    write_file(root, "flow/engine/engine.go", (
        "package engine\n\n"
        f'import "{MODULE}/leaf"\n\n'
        "var _ = leaf.Leaf\n"
    ))
    write_file(root, "flow/flow_test/flow_test.go", (
        "package flow_test\n\n"
        f'import "{MODULE}/flow"\n\n'
        "var _ = flow.Path\n"
    ))
    write_file(root, "_hidden/hidden.go", "package hidden\n\nvar _ = 1\n")
    write_file(root, "testdata/td.go", "package td\n\nvar _ = 1\n")
    write_file(root, "scripts/tool.go", "package main\n\nfunc main() {}\n")
    write_file(root, "tagged/tagged.go", (
        "//go:build ledger_sqlite\n\n"
        "package tagged\n\n"
        f'import "{MODULE}/leaf"\n\n'
        "var _ = leaf.Leaf\n"
    ))
    write_file(root, "commented/commented.go", "package commented\n\nvar _ = 1\n")
    write_file(root, "stringy/stringy.go", "package stringy\n\nvar _ = 1\n")


def _probe_enumeration(root: Path) -> list[str]:
    _write_enumeration_fixture(root)
    pkgs = packages(root, probe_env())
    problems = []
    if pkgs.get("flow/engine") != {"leaf"}:
        problems.append(f"probe_enumeration: nested package missing or wrong: {pkgs.get('flow/engine')}")
    for dropped in ("_hidden", "hidden", "testdata", "flow/flow_test", "scripts"):
        if dropped in pkgs:
            problems.append(f"probe_enumeration: expected {dropped!r} to be excluded")
    if "leaf" not in pkgs.get("tagged", set()):
        problems.append("probe_enumeration: a ledger_sqlite file must contribute its imports")
    flow = pkgs.get("flow", set())
    if flow != {"leaf"}:
        problems.append(f"probe_enumeration: flow imports must be exactly leaf, got {sorted(flow)}")
    return problems


def _probe_broken_import_block_exits(root: Path) -> list[str]:
    write_file(root, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    write_file(root, "bad/bad.go", 'package bad\n\nimport (\n\t"fmt"\n\nvar _ = fmt.Sprint\n')
    reported = io.StringIO()
    try:
        with contextlib.redirect_stderr(reported):
            packages(root, probe_env())
    except SystemExit as exit_code:
        if exit_code.code == 0:
            return ["probe_broken_import_block_exits: expected a non-zero exit"]
        if "go list failed" not in reported.getvalue():
            return ["probe_broken_import_block_exits: the go stderr must reach the caller"]
        return []
    return ["probe_broken_import_block_exits: expected the helper to exit, it returned"]


def _expect_exit(root: Path, want: str, label: str) -> list[str]:
    """_expect_exit runs the helper and requires a non-zero exit whose
    report carries `want`."""
    reported = io.StringIO()
    try:
        with contextlib.redirect_stderr(reported):
            packages(root, probe_env())
    except SystemExit as exit_code:
        if exit_code.code == 0:
            return [f"{label}: expected a non-zero exit"]
        if want not in reported.getvalue():
            return [f"{label}: expected {want!r} in the report, got {reported.getvalue()!r}"]
        return []
    return [f"{label}: expected the helper to exit, it returned"]


def _probe_unreachable_constraint_exits(root: Path) -> list[str]:
    """_probe_unreachable_constraint_exits pins the reach the two `go
    list` runs alone would lose: a file behind another constraint keeps
    its imports out of every run."""
    problems: list[str] = []

    # sub-case: a build tag outside BUILD_TAGS.
    root_a = root / "a"
    write_file(root_a, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    write_file(root_a, "secret/secret.go", "package secret\n\nvar Key = 1\n")
    write_file(root_a, "caller/caller.go", "package caller\n\nvar Open = 1\n")
    write_file(root_a, "caller/gated.go", (
        "//go:build windows\n\n"
        "package caller\n\n"
        f'import "{MODULE}/secret"\n\n'
        "var _ = secret.Key\n"
    ))
    problems.extend(_expect_exit(root_a, "build constraint names 'windows'", "probe_unreachable_constraint_exits (tag)"))

    # sub-case: a GOOS filename suffix.
    root_b = root / "b"
    write_file(root_b, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    write_file(root_b, "secret/secret.go", "package secret\n\nvar Key = 1\n")
    write_file(root_b, "caller/caller.go", "package caller\n\nvar Open = 1\n")
    write_file(root_b, "caller/caller_windows.go", (
        "package caller\n\n"
        f'import "{MODULE}/secret"\n\n'
        "var _ = secret.Key\n"
    ))
    problems.extend(_expect_exit(root_b, "filename constrains the file to windows", "probe_unreachable_constraint_exits (filename)"))

    # sub-case: every file of the directory is constrained. `go list`
    # omits such a directory, so a scan over the enumerated set alone
    # never reads it.
    root_c = root / "c"
    write_file(root_c, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    write_file(root_c, "secret/secret.go", "package secret\n\nvar Key = 1\n")
    write_file(root_c, "gonly/gated.go", (
        "//go:build windows\n\n"
        "package gonly\n\n"
        f'import "{MODULE}/secret"\n\n'
        "var _ = secret.Key\n"
    ))
    problems.extend(_expect_exit(root_c, "build constraint names 'windows'", "probe_unreachable_constraint_exits (whole directory)"))

    return problems


def _probe_real_tree_matches_go_list() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    out = subprocess.run(
        ["go", "list", "./..."], cwd=root, capture_output=True, text=True,
    )
    if out.returncode != 0:
        return [f"probe_real_tree_matches_go_list: go list failed:\n{out.stderr}"]
    prefix = MODULE + "/"
    want = set()
    for line in out.stdout.split():
        if not line.startswith(prefix):
            continue
        rel = line[len(prefix):]
        if rel == SCRIPTS or rel.startswith(SCRIPTS + "/") or rel.endswith("_test"):
            continue
        want.add(rel)
    got = set(packages(root))
    problems = []
    if got - want:
        problems.append(f"probe_real_tree_matches_go_list: extra packages {sorted(got - want)}")
    if want - got:
        problems.append(f"probe_real_tree_matches_go_list: missing packages {sorted(want - got)}")
    return problems


def run_probe() -> list[str]:
    """run_probe exercises the enumeration rules against temp-module
    fixtures and against this repo. It returns problem strings."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="gopkgs-probe-") as tmp:
        for fn in (
            _probe_enumeration,
            _probe_broken_import_block_exits,
            _probe_unreachable_constraint_exits,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))
    problems.extend(_probe_real_tree_matches_go_list())
    return problems
