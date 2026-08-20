#!/usr/bin/env python3
"""Gate: the SDK is standard library only. policy/thirdparty.json names
the packages allowed a direct exception, the modules each may import,
and the build tag (if any) an exception needs. This gate is the one
site that owns third-party truth; see docs/plans/thirdparty.md.

Seven checks: per-package imports, a residual raw-text scan for files
no `go list` pass attributes, policy shape, go.mod direct-require
equality, the go.sum closure lock, `go mod tidy -diff` identity, and
the replace/exclude/retract directive rejection."""
import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

MODULE = go_packages.MODULE
BUILD_TAGS = go_packages.BUILD_TAGS
CONFIGS = (None, ",".join(BUILD_TAGS))
REPO_ROOT = Path(__file__).resolve().parent.parent

BLOCKED_DIRECTIVES = ("replace", "exclude", "retract")
DIRECTIVE = re.compile(r"^(require|replace|exclude|retract)\b")
REQUIRE_LINE = re.compile(r"^([A-Za-z0-9._/-]+)\s+v(\S+)")
GOSUM_MODULE = re.compile(r"^(\S+)\s+")

POLICY_FIELDS = {"modules", "tag"}


def load_policy(root: Path) -> dict:
    """load_policy reads the exceptions table of thirdparty.json."""
    return json.loads((root / "policy" / "thirdparty.json").read_text())["exceptions"]


def _module_match(imp: str, module: str) -> bool:
    """_module_match: an import matches a module path when it equals
    the module path or starts with the module path plus a slash."""
    return imp == module or imp.startswith(module + "/")


def _check_imports(root: Path, env_extra: dict | None, policy: dict) -> list[str]:
    """Check one: for every build configuration, every package's
    third-party import must have a policy row whose modules list
    covers it and whose tag (if any) is active in this configuration."""
    problems: list[str] = []
    for tags in CONFIGS:
        active_tags = set((tags or "").split(",")) - {""}
        imports = go_packages.third_party_imports(root, tags, env_extra)
        for pkg, imps in sorted(imports.items()):
            for imp in sorted(imps):
                row = policy.get(pkg)
                if row is None:
                    problems.append(f"{pkg}: imports {imp}, no policy/thirdparty.json row")
                    continue
                if not any(_module_match(imp, m) for m in row.get("modules", [])):
                    problems.append(f"{pkg}: imports {imp}, outside its policy row's modules")
                    continue
                tag = row.get("tag", "")
                if tag and tag not in active_tags:
                    problems.append(
                        f"{pkg}: imports {imp} without build tag {tag} active "
                        f"(configuration: {tags or 'default'})"
                    )
    return problems


def _dot_segment(rel: Path) -> bool:
    """_dot_segment reports whether any relative path segment starts
    with a dot. rglob does not descend into a symlinked directory by
    default (Python 3.13+ recurse_symlinks=False); this matches Go's
    own default symlink handling in package discovery, so a symlinked
    tree needs no separate exclusion here."""
    return any(part.startswith(".") for part in rel.parts)


def _check_residual(root: Path, env_extra: dict | None, policy: dict) -> list[str]:
    """Check two: every .go file no `go list` pass attributes is
    scanned as raw text for a quoted policy module path. Dot-prefixed
    directories are excluded; underscore-prefixed ones are kept."""
    problems: list[str] = []
    resolved_root = root.resolve()
    attributed: set[Path] = set()
    for tags in CONFIGS:
        attributed |= go_packages.attributed_go_files(root, tags, env_extra)
    all_modules = sorted({m for row in policy.values() for m in row.get("modules", [])})
    for path in resolved_root.rglob("*.go"):
        resolved = path.resolve()
        try:
            rel = resolved.relative_to(resolved_root)
        except ValueError:
            problems.append(f"{path}: resolves outside the scanned root; cannot classify")
            continue
        if _dot_segment(rel):
            continue
        if resolved in attributed:
            continue
        text = resolved.read_text(errors="replace")
        for module in all_modules:
            if f'"{module}' in text:
                problems.append(f"{rel.as_posix()}: unattributed file names policy module {module}")
    return problems


def _check_policy_shape(root: Path, env_extra: dict | None, policy: dict) -> list[str]:
    """Check three: every row has exactly modules and tag; every tag is
    empty or a known build tag; every key names a package
    third_party_imports reports, unioned over both configurations.
    go_packages.packages is never the validation set: it drops _test
    packages and scripts."""
    problems: list[str] = []
    known_pkgs: set[str] = set()
    for tags in CONFIGS:
        known_pkgs |= set(go_packages.third_party_imports(root, tags, env_extra))
    for pkg, row in sorted(policy.items()):
        extra = set(row) - POLICY_FIELDS
        missing = POLICY_FIELDS - set(row)
        if extra or missing:
            problems.append(f"{pkg}: policy row fields must be exactly {sorted(POLICY_FIELDS)}, got {sorted(row)}")
            continue
        tag = row.get("tag", "")
        if tag and tag not in BUILD_TAGS:
            problems.append(f"{pkg}: tag {tag!r} is outside go_packages.BUILD_TAGS")
        if pkg not in known_pkgs:
            problems.append(f"{pkg}: policy row names no package third_party_imports reports")
    return problems


def _direct_requires(gomod_text: str) -> set[str]:
    """_direct_requires returns every go.mod require path without an
    `// indirect` marker."""
    out = set()
    in_require = False
    for line in gomod_text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if not in_require:
            m = DIRECTIVE.match(stripped)
            if not m:
                continue
            if m.group(1) != "require":
                continue
            if stripped.endswith("("):
                in_require = True
                continue
            stripped = stripped[len("require"):].strip()
        else:
            if stripped == ")":
                in_require = False
                continue
        if "// indirect" in stripped:
            continue
        m = REQUIRE_LINE.match(stripped)
        if m:
            out.add(m.group(1))
    return out


def _check_direct_requires(root: Path, policy: dict) -> list[str]:
    """Check four: the go.mod direct-require set must equal the union
    of every policy row's modules."""
    text = (root / "go.mod").read_text()
    requires = _direct_requires(text)
    want = {m for row in policy.values() for m in row.get("modules", [])}
    problems = []
    for extra in sorted(requires - want):
        problems.append(f"go.mod: direct require {extra} is outside every policy row's modules")
    for missing in sorted(want - requires):
        problems.append(f"go.mod: policy names module {missing}, missing a direct require")
    return problems


def _gosum_modules(text: str) -> set[str]:
    return {GOSUM_MODULE.match(line).group(1) for line in text.splitlines() if line.strip()}


def _check_closure_lock(root: Path) -> list[str]:
    """Check five: the go.sum module set must equal
    policy/thirdparty_closure.txt exactly."""
    gosum = root / "go.sum"
    lock = root / "policy" / "thirdparty_closure.txt"
    sum_modules = _gosum_modules(gosum.read_text()) if gosum.exists() else set()
    lock_modules = {line for line in lock.read_text().splitlines() if line} if lock.exists() else set()
    problems = []
    for extra in sorted(sum_modules - lock_modules):
        problems.append(f"go.sum: module {extra} is missing from policy/thirdparty_closure.txt")
    for missing in sorted(lock_modules - sum_modules):
        problems.append(f"policy/thirdparty_closure.txt: module {missing} is absent from go.sum")
    if problems:
        problems.append("run `make thirdparty-update` and commit the policy/thirdparty_closure.txt diff")
    return problems


def _check_tidy(root: Path, env_extra: dict | None) -> list[str]:
    """Check six: `go mod tidy -diff` must exit zero."""
    env = dict(os.environ)
    env.update(env_extra or {})
    out = subprocess.run(
        ["go", "mod", "tidy", "-diff"], cwd=root, capture_output=True, text=True, env=env,
    )
    if out.returncode != 0:
        return [f"go mod tidy -diff exited {out.returncode}:\n{out.stdout}{out.stderr}"]
    return []


def _check_directives(gomod: Path) -> list[str]:
    """Check seven: a replace, exclude, or retract directive is
    forbidden, unchanged from the former check_gomod.py."""
    problems = []
    in_require = False
    for n, line in enumerate(gomod.read_text().splitlines(), 1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        if in_require:
            if stripped == ")":
                in_require = False
            continue
        m = DIRECTIVE.match(stripped)
        if not m:
            continue
        directive = m.group(1)
        if directive in BLOCKED_DIRECTIVES:
            problems.append(f"go.mod:{n}: directive {directive} is forbidden; standard library only")
            continue
        if directive == "require" and stripped.endswith("("):
            in_require = True
    return problems


def check(root: Path, env_extra: dict | None = None) -> list[str]:
    """check runs all seven checks against one repo root. Returns
    problem strings; empty means the gate passes."""
    gomod = root / "go.mod"
    if not gomod.exists():
        return ["go.mod: missing"]
    policy = load_policy(root)
    problems: list[str] = []
    problems.extend(_check_imports(root, env_extra, policy))
    problems.extend(_check_residual(root, env_extra, policy))
    problems.extend(_check_policy_shape(root, env_extra, policy))
    problems.extend(_check_direct_requires(root, policy))
    problems.extend(_check_closure_lock(root))
    problems.extend(_check_tidy(root, env_extra))
    problems.extend(_check_directives(gomod))
    # go_packages.packages runs the build-constraint scan, which fails
    # closed (sys.exit) on a file behind an unlisted constraint. Check
    # two covers non-enumerated directories; this covers the rest.
    go_packages.packages(root, env_extra)
    return problems


# --- probes ---------------------------------------------------------


def _write_base(root: Path) -> None:
    """_write_base mirrors the real repo's third-party exception
    structure: the real go.mod, go.sum, and policy files verbatim,
    plus one stub file per excepted package that imports exactly what
    the real repo's require closure needs. `go mod tidy -diff` is
    zero over this fixture, matching the real tree, so every probe
    below starts from a known-tidy state and only perturbs the one
    thing it means to test."""
    go_packages.write_file(root, "go.mod", (REPO_ROOT / "go.mod").read_text())
    go_packages.write_file(root, "go.sum", (REPO_ROOT / "go.sum").read_text())
    go_packages.write_file(root, "policy/thirdparty.json", (REPO_ROOT / "policy" / "thirdparty.json").read_text())
    go_packages.write_file(
        root, "policy/thirdparty_closure.txt",
        (REPO_ROOT / "policy" / "thirdparty_closure.txt").read_text(),
    )
    go_packages.write_file(root, "a2aclient/a2aclient.go", (
        "package a2aclient\n\nimport (\n"
        '\t_ "github.com/a2aproject/a2a-go/a2a"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2aclient"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2agrpc"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2asrv"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2asrv/eventqueue"\n'
        '\t_ "google.golang.org/grpc"\n'
        ")\n"
    ))
    go_packages.write_file(root, "a2aloopback/a2aloopback.go", (
        "package a2aloopback\n\nimport (\n"
        '\t_ "github.com/a2aproject/a2a-go/a2a"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2asrv"\n'
        '\t_ "github.com/a2aproject/a2a-go/a2asrv/eventqueue"\n'
        '\t_ "google.golang.org/grpc"\n'
        ")\n"
    ))
    go_packages.write_file(root, "mcp/mcp.go", (
        'package mcp\n\nimport _ "github.com/modelcontextprotocol/go-sdk/mcp"\n\nvar _ = 1\n'
    ))
    go_packages.write_file(root, "ledger/ledger.go", (
        "//go:build ledger_sqlite\n\n"
        'package ledger\n\nimport _ "modernc.org/sqlite"\n\nvar _ = 1\n'
    ))
    go_packages.write_file(root, "schema/schema.go", (
        'package schema\n\nimport _ "github.com/santhosh-tekuri/jsonschema/v6"\n\nvar _ = 1\n'
    ))


def _probe_wrong_package_fails(root: Path) -> list[str]:
    """Adversarial: a package with a policy row imports a module its
    row does not list."""
    _write_base(root)
    go_packages.write_file(root, "mcp/extra.go", (
        'package mcp\n\nimport _ "modernc.org/sqlite"\n\nvar _ = 1\n'
    ))
    problems = check(root, go_packages.probe_env())
    if not any("mcp" in p and "modernc.org/sqlite" in p for p in problems):
        return [f"probe_wrong_package_fails: expected an mcp/sqlite problem, got {problems}"]
    return []


def _probe_unscoped_package_fails(root: Path) -> list[str]:
    """The fail-open case: a package with no policy row imports a
    module another row lists. The old exclude-list design allowed it."""
    _write_base(root)
    go_packages.write_file(root, "other/other.go", (
        'package other\n\nimport _ "github.com/modelcontextprotocol/go-sdk/mcp"\n\nvar _ = 1\n'
    ))
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("other:") and "no policy/thirdparty.json row" in p for p in problems):
        return [f"probe_unscoped_package_fails: expected an unscoped-package problem, got {problems}"]
    return []


def _probe_excepted_but_unscoped_fails(root: Path) -> list[str]:
    """Pins the fail-open hole shut: a package the deleted exclude
    list would have covered, with its policy row removed, still
    imports its module."""
    _write_base(root)
    policy = load_policy(root)
    del policy["schema"]
    go_packages.write_file(root, "policy/thirdparty.json", json.dumps({
        "comment": "probe fixture", "exceptions": policy,
    }))
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("schema:") and "no policy/thirdparty.json row" in p for p in problems):
        return [f"probe_excepted_but_unscoped_fails: expected a schema problem, got {problems}"]
    return []


def _probe_missing_tag_fails(root: Path) -> list[str]:
    """The untagged pass must report a tag-gated module imported from
    a file with no build tag."""
    _write_base(root)
    go_packages.write_file(root, "ledger/extra.go", (
        'package ledger\n\nimport _ "modernc.org/sqlite"\n\nvar _ = 1\n'
    ))
    problems = check(root, go_packages.probe_env())
    if not any("without build tag ledger_sqlite" in p for p in problems):
        return [f"probe_missing_tag_fails: expected a missing-tag problem, got {problems}"]
    return []


def _probe_foreign_module_in_gomod_fails(root: Path) -> list[str]:
    """A require line for a module path outside the union of every
    policy row's modules. The extra module is never imported, so `go
    list` stays offline-safe; only its go.mod text and its tidy state
    are affected."""
    _write_base(root)
    gomod = (root / "go.mod").read_text()
    gomod = gomod.replace(
        "require (\n\tgithub.com/a2aproject/a2a-go",
        "require (\n\tgithub.com/other/foreign v0.0.1\n\tgithub.com/a2aproject/a2a-go",
    )
    go_packages.write_file(root, "go.mod", gomod)
    with (root / "go.sum").open("a") as f:
        f.write("github.com/other/foreign v0.0.1 h1:aaaa=\n")
        f.write("github.com/other/foreign v0.0.1/go.mod h1:bbbb=\n")
    lock = (root / "policy" / "thirdparty_closure.txt").read_text()
    go_packages.write_file(root, "policy/thirdparty_closure.txt", lock + "github.com/other/foreign\n")
    problems = check(root, go_packages.probe_env())
    if not any("github.com/other/foreign" in p and "outside every policy row" in p for p in problems):
        return [f"probe_foreign_module_in_gomod_fails: expected a foreign-module problem, got {problems}"]
    return []


def _probe_closure_drift_fails(root: Path) -> list[str]:
    """Closure drift, both directions: a go.sum module path absent
    from the lock, and a lock line absent from go.sum."""
    _write_base(root)
    go_packages.write_file(root, "policy/thirdparty_closure.txt", "stale.example.com/module\n")
    problems = check(root, go_packages.probe_env())
    if not any("missing from policy/thirdparty_closure.txt" in p for p in problems):
        return [f"probe_closure_drift_fails: expected a missing-from-lock problem, got {problems}"]
    if not any("absent from go.sum" in p for p in problems):
        return [f"probe_closure_drift_fails: expected an absent-from-go.sum problem, got {problems}"]
    return []


def _probe_untidy_fails(root: Path) -> list[str]:
    """A stray, unused require line makes `go mod tidy -diff` exit
    non-zero. golang.org/x/mod is already in the closure at a known
    version, so no new go.sum hash is invented."""
    _write_base(root)
    gomod = (root / "go.mod").read_text()
    m = re.search(r"golang\.org/x/mod v([0-9.]+)", (REPO_ROOT / "go.sum").read_text())
    version = m.group(1) if m else "0.0.0"
    gomod = gomod.replace(
        "require (\n\tgithub.com/a2aproject/a2a-go",
        f"require (\n\tgolang.org/x/mod v{version}\n\tgithub.com/a2aproject/a2a-go",
    )
    go_packages.write_file(root, "go.mod", gomod)
    problems = check(root, go_packages.probe_env())
    if not any("go mod tidy -diff exited" in p for p in problems):
        return [f"probe_untidy_fails: expected a tidy-diff problem, got {problems}"]
    return []


def _probe_directive_fails(root: Path) -> list[str]:
    """A replace, exclude, or retract directive is forbidden. The
    targeted module is never required or imported, so `go list` stays
    offline-safe regardless of the directive's own validity."""
    problems_all = []
    directive_lines = {
        "replace": "replace github.com/other/fork => github.com/other/fork2 v0.0.1",
        "exclude": "exclude github.com/other/fork v0.0.1",
        "retract": "retract v0.0.1",
    }
    for directive, line in directive_lines.items():
        sub = root / directive
        go_packages.write_file(sub, "go.mod", (
            f"module {MODULE}\n\ngo 1.25.0\n\n{line}\n\n"
            "require github.com/modelcontextprotocol/go-sdk v1.7.0\n"
        ))
        go_packages.write_file(sub, "go.sum", (REPO_ROOT / "go.sum").read_text())
        go_packages.write_file(sub, "mcp/mcp.go", "package mcp\n\nvar Ok = 1\n")
        go_packages.write_file(sub, "policy/thirdparty.json", json.dumps({
            "comment": "probe fixture",
            "exceptions": {"mcp": {"modules": ["github.com/modelcontextprotocol/go-sdk"], "tag": ""}},
        }))
        go_packages.write_file(sub, "policy/thirdparty_closure.txt", (REPO_ROOT / "policy" / "thirdparty_closure.txt").read_text())
        problems = check(sub, go_packages.probe_env())
        if not any(f"directive {directive} is forbidden" in p for p in problems):
            problems_all.append(f"probe_directive_fails ({directive}): expected a rejection, got {problems}")
    return problems_all


def _probe_bad_tag_fails(root: Path) -> list[str]:
    _write_base(root)
    policy = load_policy(root)
    policy["mcp"]["tag"] = "windows"
    go_packages.write_file(root, "policy/thirdparty.json", json.dumps({
        "comment": "probe fixture", "exceptions": policy,
    }))
    problems = check(root, go_packages.probe_env())
    if not any("outside go_packages.BUILD_TAGS" in p for p in problems):
        return [f"probe_bad_tag_fails: expected a bad-tag problem, got {problems}"]
    return []


def _probe_bad_field_fails(root: Path) -> list[str]:
    _write_base(root)
    policy = load_policy(root)
    policy["mcp"]["extra"] = "x"
    go_packages.write_file(root, "policy/thirdparty.json", json.dumps({
        "comment": "probe fixture", "exceptions": policy,
    }))
    problems = check(root, go_packages.probe_env())
    if not any("policy row fields must be exactly" in p for p in problems):
        return [f"probe_bad_field_fails: expected a bad-field problem, got {problems}"]
    return []


def _probe_stale_row_fails(root: Path) -> list[str]:
    """A key naming no package third_party_imports reports."""
    _write_base(root)
    policy = load_policy(root)
    policy["nosuchpkg"] = {"modules": ["github.com/modelcontextprotocol/go-sdk"], "tag": ""}
    go_packages.write_file(root, "policy/thirdparty.json", json.dumps({
        "comment": "probe fixture", "exceptions": policy,
    }))
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("nosuchpkg:") and "no package third_party_imports reports" in p for p in problems):
        return [f"probe_stale_row_fails: expected a stale-row problem, got {problems}"]
    return []


def _probe_residual_classes_fail(root: Path) -> list[str]:
    """One probe per residual-file class from the plan's Tests section.
    Each hides an import of a policy module path from every `go list`
    pass, and each must fail the gate."""
    problems: list[str] = []

    def fresh(name: str) -> Path:
        sub = root / name
        _write_base(sub)
        return sub

    # A _test.go file behind a filename constraint.
    r = fresh("filename_constraint")
    go_packages.write_file(r, "mcp/hidden_windows_test.go", (
        'package mcp\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nvar _ = 1\n'
    ))
    p = check(r, go_packages.probe_env())
    if not any("hidden_windows_test.go" in x for x in p):
        problems.append(f"residual(filename constraint): expected a hit, got {p}")

    # A file in an external _test directory behind a build tag.
    r = fresh("tagged_external_test_dir")
    go_packages.write_file(r, "mcp/mcp_test/fixture.go", (
        "//go:build windows\n\n"
        'package mcp_test\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nvar _ = 1\n'
    ))
    p = check(r, go_packages.probe_env())
    if not any("mcp_test/fixture.go" in x for x in p):
        problems.append(f"residual(external test dir): expected a hit, got {p}")

    # A file under an underscore-prefixed directory.
    r = fresh("underscore_dir")
    go_packages.write_file(r, "docs/examples/_agentcomposition/main.go", (
        'package main\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nfunc main() {}\n'
    ))
    p = check(r, go_packages.probe_env())
    if not any("_agentcomposition/main.go" in x for x in p):
        problems.append(f"residual(underscore dir): expected a hit, got {p}")

    # A file under testdata/.
    r = fresh("testdata_dir")
    go_packages.write_file(r, "mcp/testdata/fixture.go", (
        'package testdata\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nvar _ = 1\n'
    ))
    p = check(r, go_packages.probe_env())
    if not any("mcp/testdata/fixture.go" in x for x in p):
        problems.append(f"residual(testdata): expected a hit, got {p}")

    # A file under scripts/, behind a //go:build windows constraint.
    # scripts is a real package in go list ./..., so an unconstrained
    # file would be attributed, never residual.
    r = fresh("scripts_dir")
    go_packages.write_file(r, "scripts/hidden_windows.go", (
        "//go:build windows\n\n"
        'package main\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nfunc main() {}\n'
    ))
    p = check(r, go_packages.probe_env())
    if not any("scripts/hidden_windows.go" in x for x in p):
        problems.append(f"residual(scripts dir): expected a hit, got {p}")

    # A root path that itself holds a dot segment, with the violation
    # planted at <tmp>/.agentwork/<module>/testdata/leak.go: the dot
    # root, plus the plan's testdata/ safe class, so the fixture stays
    # invisible to go_packages.packages()'s constraint-scan sys.exit.
    dotroot = root / ".agentwork" / "leakmod"
    _write_base(dotroot)
    go_packages.write_file(dotroot, "mcp/testdata/leak.go", (
        'package testdata\n\nimport _ "github.com/modelcontextprotocol/go-sdk"\n\nvar _ = 1\n'
    ))
    p = check(dotroot, go_packages.probe_env())
    if not any("leak.go" in x for x in p):
        problems.append(f"residual(dot root): expected a hit, got {p}")

    return problems


def _probe_tagged_import_passes(root: Path) -> list[str]:
    """Cases that must pass: the tagged pass allows a tag-scoped
    import in the scoped package."""
    _write_base(root)
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_tagged_import_passes: expected pass, got {problems}"]
    return []


def _probe_subpath_import_passes(root: Path) -> list[str]:
    """A sub-path import matches its module entry by prefix."""
    _write_base(root)
    go_packages.write_file(root, "a2aclient/eventqueue_extra.go", (
        'package a2aclient\n\nimport _ "github.com/a2aproject/a2a-go/a2asrv/eventqueue"\n\nvar _ = 1\n'
    ))
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_subpath_import_passes: expected pass, got {problems}"]
    return []


def _probe_stdlib_and_internal_never_third_party(root: Path) -> list[str]:
    """A standard library import and an import of this module are
    never third-party."""
    _write_base(root)
    go_packages.write_file(root, "leaf/leaf.go", "package leaf\n\nvar Leaf = 1\n")
    go_packages.write_file(root, "mcp/extra.go", (
        'package mcp\n\nimport (\n\t"encoding/json"\n\n'
        f'\t"{MODULE}/leaf"\n)\n\nvar _ = json.RawMessage(nil)\nvar _ = leaf.Leaf\n'
    ))
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_stdlib_and_internal_never_third_party: expected pass, got {problems}"]
    return []


def _probe_attributed_never_residual(root: Path) -> list[str]:
    """A legitimate import never fires check two. This also catches a
    broken path normalization, which would turn every attributed file
    residual."""
    _write_base(root)
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_attributed_never_residual: expected pass, got {problems}"]
    return []


def _probe_dot_directory_pass_and_fail(root: Path) -> list[str]:
    """The discriminating dot-root pair: a dot-prefixed directory
    holding a policy module path is skipped (pass case), while the
    dot-root residual fixture above (fail case) proves the exclusion
    is not silently disabling the whole scan. Both are needed
    together."""
    _write_base(root)
    go_packages.write_file(root, ".worktree/a2aclient/grpc.go", (
        'package a2aclient\n\nimport _ "google.golang.org/grpc"\n\nvar _ = 1\n'
    ))
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_dot_directory_pass_and_fail: expected pass, got {problems}"]
    return []


def _probe_real_tree_passes() -> list[str]:
    problems = check(REPO_ROOT)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def _probe_external_test_dir_reach(root: Path) -> list[str]:
    """A third-party import inside an external _test package directory
    is seen, and needs that directory's own policy row."""
    _write_base(root)
    go_packages.write_file(root, "mcp/mcp_test/fixture_test.go", (
        f'package mcp_test\n\nimport (\n\t"testing"\n\n\t"{MODULE}/mcp"\n\n'
        '\t_ "github.com/modelcontextprotocol/go-sdk"\n)\n\n'
        "func TestOk(t *testing.T) { _ = 1 }\n"
    ))
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("mcp/mcp_test:") for p in problems):
        return [f"probe_external_test_dir_reach: expected an mcp/mcp_test problem, got {problems}"]
    return []


def _probe_in_package_test_reach(root: Path) -> list[str]:
    """A third-party import inside an in-package _test.go file is seen
    through TestImports."""
    _write_base(root)
    go_packages.write_file(root, "other/other.go", "package other\n\nvar Ok = 1\n")
    go_packages.write_file(root, "other/other_test.go", (
        'package other\n\nimport (\n\t"testing"\n\n\t_ "github.com/modelcontextprotocol/go-sdk"\n)\n\n'
        "func TestOk(t *testing.T) { _ = Ok }\n"
    ))
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("other:") for p in problems):
        return [f"probe_in_package_test_reach: expected an other problem, got {problems}"]
    return []



def run_probe() -> bool:
    """run_probe exercises the seven checks against temp-module
    fixtures and against this repo."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="thirdparty-probe-") as tmp:
        for fn in (
            _probe_wrong_package_fails,
            _probe_unscoped_package_fails,
            _probe_excepted_but_unscoped_fails,
            _probe_missing_tag_fails,
            _probe_foreign_module_in_gomod_fails,
            _probe_closure_drift_fails,
            _probe_untidy_fails,
            _probe_directive_fails,
            _probe_bad_tag_fails,
            _probe_bad_field_fails,
            _probe_stale_row_fails,
            _probe_residual_classes_fail,
            _probe_tagged_import_passes,
            _probe_subpath_import_passes,
            _probe_stdlib_and_internal_never_third_party,
            _probe_attributed_never_residual,
            _probe_dot_directory_pass_and_fail,
            _probe_external_test_dir_reach,
            _probe_in_package_test_reach,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))
    problems.extend(_probe_real_tree_passes())
    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="third-party import exception gate")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
