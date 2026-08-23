#!/usr/bin/env python3
"""Gate: every Go package needs a plan at docs/plans/<path>.md with the
sections from docs/plans/TEMPLATE.md. The path is the package path
relative to the module root, so a nested package needs a nested plan
file. The plan is where an agent declares the package's goal, scope,
API, tests, and verification BEFORE or WITH the code; the gate makes the
structure non-optional.

The Tests section is checked twice. First, the section header is
present (the structural rule). Second, every Test-named token written
in plain prose in the section names a Go test function declared in the
package's own `*_test.go` files (the commitment rule). Tokens written
inside single-backtick inline spans are references, not claims: the
gate does not look for them as live tests. Tokens inside triple-backtick
fenced blocks are still candidates for the cross-check, because a
fence is the natural place to list the package's own test names. See
`_strip_code_spans`.

The same two checks apply to every `### Addendum tests` subsection
that appears inside a plan's `## Addendum:` blocks. Each addendum
test section is checked independently, with its own missing-test
problems. The heading text is embedded in the problem string so a
grep can distinguish addendum cross-check failures from top-level
`## Tests` cross-check failures.

A package tree with zero `*_test.go` files (including zero in
`<pkg>/<pkg>_test/`) skips the cross-check: there is nothing to
compare the section's claims against. The structural section check
still applies."""
import argparse
import re
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

REQUIRED = ["## Goal", "## Scope", "## API", "## Tests", "## Verification"]

# TEST_NAME matches a whole Go test function identifier: the `Test`
# prefix followed by an uppercase letter and one or more word
# characters. The `\b` anchors keep partial matches (e.g. a word that
# contains "Test" mid-token) out of the gate.
TEST_NAME = re.compile(r"\bTest[A-Z]\w+")
TEST_FUNC = re.compile(r"^func\s+(Test[A-Z]\w+)\s*\(")
# Two narrow patterns beat one alternation: the body boundaries differ.
# `## Tests` is a top-level section, so its body stops at the next
# `## ` line. `### Addendum tests` is a sibling-level subsection, so
# its body stops at the next `### ` line, the natural sibling break,
# which avoids swallowing the following `### Addendum verification`
# block into the tests body.
_PATTERN_TOP_TESTS = re.compile(r"(?ms)^## Tests\s*$\n(.*?)(?=^## |\Z)")
_PATTERN_ADDENDUM_TESTS = re.compile(
    r"(?ms)^### Addendum tests\s*$\n(.*?)(?=^### |\Z)"
)


_FENCE_LINE = re.compile(r"^\s*(`{3,}|~{3,})")


def _strip_code_spans(text: str) -> str:
    """_strip_code_spans scrubs single-backtick inline spans from the
    prose, leaving triple-backtick fenced blocks untouched.

    Fence behavior: triple-backtick fences are preserved as-is so tokens
    inside triple-fence lines remain candidates for the cross-check.
    Single-backtick inline spans are scrubbed.

    The walker splits the text on newline boundaries and tracks fence
    state. A line that opens a fence (matches ``^\\s*`{3,}`` or the
    tilde form) flips the state to "open". A later line whose fence
    marker matches the opener flips the state back to "closed". Lines
    emitted while the fence is open pass through verbatim, so test
    names listed inside a code block still reach TEST_NAME. Lines
    emitted while the fence is closed have paired single-backtick
    spans removed by `_scrub_inline_backticks`.

    An unmatched trailing single backtick on a non-fence line is
    harmless: nothing follows it to pair with, so the rest of the
    line is left as-is.
    """
    out: list[str] = []
    in_fence = False
    fence_marker: str | None = None
    for line in text.splitlines(keepends=True):
        m = _FENCE_LINE.match(line)
        if in_fence:
            if m and m.group(1)[0] == fence_marker:
                out.append(line)
                in_fence = False
                fence_marker = None
            else:
                out.append(line)
            continue
        if m:
            fence_marker = m.group(1)[0]
            in_fence = True
            out.append(line)
            continue
        out.append(_scrub_inline_backticks(line))
    return "".join(out)


def _scrub_inline_backticks(line: str) -> str:
    """_scrub_inline_backticks removes the contents of every paired
    single-backtick span on one line, leaving the surrounding prose.
    Pairing is left-to-right, so `` `a` `b` `` becomes `` ` ` `` with
    both spans scrubbed."""
    out: list[str] = []
    i = 0
    while i < len(line):
        if line[i] == "`":
            j = line.find("`", i + 1)
            if j == -1:
                out.append(line[i:])
                return "".join(out)
            i = j + 1
            continue
        out.append(line[i])
        i += 1
    return "".join(out)


def _collect_tests_sections(plan_text: str) -> list[tuple[str, str]]:
    """_collect_tests_sections returns [(body, heading_label), ...] for
    every cross-checked Tests section in the plan, in document order.
    heading_label is the literal heading the problem string embeds,
    e.g. `## Tests` or `### Addendum tests`. A plan with one top
    section and N addendum subsections produces 1 + N entries; the
    same package's `_test.go` set is checked against every entry."""
    sections: list[tuple[str, str]] = []
    for match in _PATTERN_TOP_TESTS.finditer(plan_text):
        sections.append((match.group(1), "## Tests"))
    for match in _PATTERN_ADDENDUM_TESTS.finditer(plan_text):
        sections.append((match.group(1), "### Addendum tests"))
    return sections


def _declared_tests(pkg_dir: Path) -> set[str]:
    """_declared_tests returns the names of every Go test function
    declared in the package's own `*_test.go` files, plus any test
    function declared in the matching `<pkg>/<pkg>_test/` subpackage.
    `go list` enumerates that subpackage as its own package, but it
    is the canonical home for the package's white-box integration
    tests: every package in this module follows the `<pkg>/<pkg>_test/`
    naming convention, so the gate accepts a token named in the plan
    when it lives in either place.
    """
    names: set[str] = set()
    candidates = [pkg_dir]
    test_subpkg = pkg_dir / f"{pkg_dir.name}_test"
    if test_subpkg.is_dir():
        candidates.append(test_subpkg)
    for dir_ in candidates:
        if not dir_.is_dir():
            continue
        for path in dir_.glob("*_test.go"):
            for line in path.read_text().splitlines():
                match = TEST_FUNC.match(line)
                if match:
                    names.add(match.group(1))
    return names


def check(root: Path, env_extra: dict | None = None) -> list[str]:
    """check runs the plan gate against one repo root. Returns problem
    strings; empty means the gate passes.

    A package tree with zero `*_test.go` files (including zero in
    `<pkg>/<pkg>_test/`) means the cross-check has nothing to compare
    against. The gate skips the cross-check for that package; the
    structural section check still applies."""
    problems: list[str] = []
    for pkg in go_packages.package_paths(root, env_extra):
        plan = root / "docs" / "plans" / f"{pkg}.md"
        if not plan.exists():
            problems.append(f"{pkg}: no plan; create docs/plans/{pkg}.md from TEMPLATE.md")
            continue
        text = plan.read_text()
        for section in REQUIRED:
            if not re.search(rf"^{re.escape(section)}\s*$", text, re.M):
                problems.append(f"{pkg}: plan lacks section {section!r}")
        sections = _collect_tests_sections(text)
        if not sections:
            continue
        declared = _declared_tests(root / pkg)
        if not declared:
            continue
        for body, heading in sections:
            scrubbed = _strip_code_spans(body)
            named = sorted(set(TEST_NAME.findall(scrubbed)))
            if not named:
                continue
            missing = [name for name in named if name not in declared]
            for name in missing:
                problems.append(
                    f"{pkg}: test {name!r} named in docs/plans/{pkg}.md "
                    f"{heading} has no func declaration in any "
                    f"{pkg}/*_test.go"
                )
    return problems


# --- probes ---------------------------------------------------------


def _write_fixture(root: Path) -> None:
    """_write_fixture writes a module holding one nested package."""
    go_packages.write_file(root, "go.mod", f"module {go_packages.MODULE}\n\ngo 1.25.0\n")
    go_packages.write_file(root, "flow/engine/engine.go", "package engine\n\nvar Run = 1\n")


def _plan_text(sections: list[str]) -> str:
    return "# Plan\n\n" + "\n\n".join(f"{s}\n\nText.\n" for s in sections)


def _probe_nested_without_plan_fails(root: Path) -> list[str]:
    _write_fixture(root)
    problems = check(root, go_packages.probe_env())
    if not any("flow/engine: no plan" in p for p in problems):
        return [f"probe_nested_without_plan_fails: expected a no-plan problem, got {problems}"]
    return []


def _probe_nested_with_plan_passes(root: Path) -> list[str]:
    _write_fixture(root)
    go_packages.write_file(root, "docs/plans/flow/engine.md", _plan_text(REQUIRED))
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_nested_with_plan_passes: expected pass, got {problems}"]
    return []


def _probe_missing_section_fails(root: Path) -> list[str]:
    _write_fixture(root)
    go_packages.write_file(root, "docs/plans/flow/engine.md", _plan_text(REQUIRED[:-1]))
    problems = check(root, go_packages.probe_env())
    if not any("plan lacks section '## Verification'" in p for p in problems):
        return [f"probe_missing_section_fails: expected a missing-section problem, got {problems}"]
    return []


def _probe_backticked_cross_ref_passes(root: Path) -> list[str]:
    """A `## Tests` body that lists `` `TestCrossPkgRef` `` (in backticks)
    and `TestDirectRef` (plain prose), with the package declaring only
    `TestDirectRef`, must pass: the backticked token is a reference,
    not a claim, and the plain-prose token names a real test."""
    _write_fixture(root)
    go_packages.write_file(root, "flow/engine/engine_test.go", "package engine\n\nfunc TestDirectRef(t *testing.T) {}\n")
    plan = (
        "# Plan\n\n"
        "## Goal\n\nText.\n\n"
        "## Scope\n\nText.\n\n"
        "## API\n\nText.\n\n"
        "## Tests\n\n"
        "Names one test in this package: TestDirectRef.\n"
        "Cross-package reference: `TestCrossPkgRef`.\n\n"
        "## Verification\n\nText.\n"
    )
    go_packages.write_file(root, "docs/plans/flow/engine.md", plan)
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_backticked_cross_ref_passes: expected pass, got {problems}"]
    return []


def _probe_no_test_files_skips(root: Path) -> list[str]:
    """A package with no `*_test.go` files anywhere in its tree and a
    plan that names a Test function in its `## Tests` section must not
    raise a missing-declaration problem: there is nothing to compare
    against, so the cross-check has nothing to do."""
    _write_fixture(root)
    plan = (
        "# Plan\n\n"
        "## Goal\n\nText.\n\n"
        "## Scope\n\nText.\n\n"
        "## API\n\nText.\n\n"
        "## Tests\n\n"
        "Names one test that has no source file: TestNeverLanded.\n\n"
        "## Verification\n\nText.\n"
    )
    go_packages.write_file(root, "docs/plans/flow/engine.md", plan)
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_no_test_files_skips: expected pass, got {problems}"]
    return []


def _probe_addendum_tests_checked(root: Path) -> list[str]:
    """A plan whose `## Tests` lists `TestDirectRef` and whose
    `### Addendum tests` lists `TestAddendumClaim` must fire on the
    addendum missing-test. `TestDirectRef` is declared in `_test.go`;
    `TestAddendumClaim` is not. The probe proves the addendum
    cross-check runs alongside the top-level check."""
    _write_fixture(root)
    go_packages.write_file(
        root,
        "flow/engine/engine_test.go",
        "package engine\n\nfunc TestDirectRef(t *testing.T) {}\n",
    )
    plan = (
        "# Plan\n\n"
        "## Goal\n\nText.\n\n"
        "## Scope\n\nText.\n\n"
        "## API\n\nText.\n\n"
        "## Tests\n\n"
        "Names one test in this package: TestDirectRef.\n\n"
        "## Verification\n\nText.\n\n"
        "## Addendum: feature shipped\n\n"
        "### Addendum tests\n\n"
        "Names the addendum claim: TestAddendumClaim.\n\n"
        "### Addendum verification\n\n"
        "make verify passes.\n"
    )
    go_packages.write_file(root, "docs/plans/flow/engine.md", plan)
    problems = check(root, go_packages.probe_env())
    if not any(
        "TestAddendumClaim" in p and "### Addendum tests" in p for p in problems
    ):
        return [
            "probe_addendum_tests_checked: expected an addendum "
            "missing-test problem for TestAddendumClaim, got "
            f"{problems}"
        ]
    if any("TestDirectRef" in p for p in problems):
        return [
            "probe_addendum_tests_checked: TestDirectRef is declared "
            "and must not appear in problems, got "
            f"{problems}"
        ]
    return []


def _probe_addendum_tests_passes_when_declared(root: Path) -> list[str]:
    """Same fixture as `_probe_addendum_tests_checked`. Both tests
    declared. The gate must produce zero problems, proving the
    addendum cross-check passes when the claim matches reality."""
    _write_fixture(root)
    go_packages.write_file(
        root,
        "flow/engine/engine_test.go",
        (
            "package engine\n\n"
            "func TestDirectRef(t *testing.T) {}\n\n"
            "func TestAddendumClaim(t *testing.T) {}\n"
        ),
    )
    plan = (
        "# Plan\n\n"
        "## Goal\n\nText.\n\n"
        "## Scope\n\nText.\n\n"
        "## API\n\nText.\n\n"
        "## Tests\n\n"
        "Names one test in this package: TestDirectRef.\n\n"
        "## Verification\n\nText.\n\n"
        "## Addendum: feature shipped\n\n"
        "### Addendum tests\n\n"
        "Names the addendum claim: TestAddendumClaim.\n\n"
        "### Addendum verification\n\n"
        "make verify passes.\n"
    )
    go_packages.write_file(root, "docs/plans/flow/engine.md", plan)
    problems = check(root, go_packages.probe_env())
    if problems:
        return [
            "probe_addendum_tests_passes_when_declared: expected pass, "
            f"got {problems}"
        ]
    return []


def _probe_real_tree_passes() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the plan gate against temp-module fixtures,
    following check_mutation.py's --probe convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="plan-probe-") as tmp:
        for fn in (
            _probe_nested_without_plan_fails,
            _probe_nested_with_plan_passes,
            _probe_missing_section_fails,
            _probe_backticked_cross_ref_passes,
            _probe_no_test_files_skips,
            _probe_addendum_tests_checked,
            _probe_addendum_tests_passes_when_declared,
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
    parser = argparse.ArgumentParser(description="package plan gate")
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
