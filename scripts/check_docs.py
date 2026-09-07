#!/usr/bin/env python3
"""Gate: every exported top-level Go symbol needs a doc comment that
starts with the symbol name. Keeps the package surface self-describing
for AI and human consumers. Exits non-zero on violations."""
import re
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

DECL = re.compile(r"^(?:func\s+(?:\([^)]*\)\s+)?|type\s+)([A-Z]\w*)")

# A docs/packages/<path>.md reference page must name a live package,
# unless its first "Status:" line marks it superseded or relocated.
# This keeps a fold or a removal from leaving a stale reference page
# behind: check_orphan_packages.py catches the missing wiring, this
# gate catches the missing doc cleanup.
_PACKAGE_DOC_STATUS = re.compile(r"^\s*(?:>\s*)?Status:\s*(\S+)", re.MULTILINE)
_PACKAGE_DOC_ALLOWED_STATUS = {"superseded", "relocated", "moved"}

# The package-count rule pins docs/architecture.md's opening paragraph
# to the tree: the spelled-out count in "the <words> packages" must
# equal go list's package count. This drift recurred three times before
# the gate existed.
_UNITS = {
    "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
    "seven": 7, "eight": 8, "nine": 9,
}
_TENS = {
    "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13,
    "fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17,
    "eighteen": 18, "nineteen": 19,
    "twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
    "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
}
_COUNT_PHRASE = re.compile(r"\b([a-z]+(?:-[a-z]+)*)\s+packages\b")


def words_to_count(words: str):
    """words_to_count parses a spelled-out count such as "forty-nine"
    or "twenty" into an int. None means the words are not a supported
    count."""
    total = 0
    seen = False
    for part in words.split("-"):
        if part in _UNITS:
            total += _UNITS[part]
            seen = True
        elif part in _TENS:
            total += _TENS[part]
            seen = True
        else:
            return None
    return total if seen else None


def doc_package_count(text: str):
    """doc_package_count returns architecture.md's spelled-out package
    count from its opening paragraph. None means the phrase is
    missing."""
    for line in text.splitlines()[:40]:
        m = _COUNT_PHRASE.search(line)
        if m:
            return words_to_count(m.group(1))
    return None


def go_package_count(root: Path) -> int:
    """go_package_count counts the module's non-test packages the same
    way the docs do: go list ./..., one count per package."""
    out = subprocess.run(
        ["go", "list", "./..."], cwd=root, capture_output=True, text=True, check=True
    )
    # A "<pkg>/<pkg>_test" subpackage is the white-box test home, not a
    # package the architecture diagram shows; the docs count excludes it.
    return sum(
        1
        for line in out.stdout.splitlines()
        if line.strip() and not line.strip().endswith("_test")
    )


def check_package_count(root: Path) -> list[str]:
    """check_package_count applies the count rule. A missing phrase or
    an unparsable count is a problem, not a skip: the sentence is
    load-bearing."""
    problems = []
    doc = root / "docs" / "architecture.md"
    if not doc.exists():
        return ["docs/architecture.md: missing; the package-count rule has no target"]
    stated = doc_package_count(doc.read_text())
    if stated is None:
        return [
            "docs/architecture.md: the opening paragraph states no "
            "spelled-out package count; write 'the <count> packages'"
        ]
    actual = go_package_count(root)
    if stated != actual:
        problems.append(
            f"docs/architecture.md: states {stated} packages, "
            f"go list counts {actual}; run go list ./... and fix the sentence"
        )
    return problems


def package_doc_status(text: str) -> str | None:
    """package_doc_status returns the first "Status:" line's first word,
    lowercased. None means the page states no status line."""
    m = _PACKAGE_DOC_STATUS.search(text)
    if not m:
        return None
    return m.group(1).strip(",.").lower()


def check_package_doc_paths(root: Path) -> list[str]:
    """check_package_doc_paths flags a docs/packages/<path>.md page whose
    <path> names no live package. A page for a moved or deleted package
    must say so with a leading "Status: superseded" or "Status:
    relocated" line; an ordinary reference page must name a real
    package."""
    problems = []
    live = set(go_packages.package_paths(root))
    # A page may use a package's last path segment as its flat basename
    # instead of nesting the full path, e.g. docs/packages/ref.md for
    # the context/ref package (see docs/README.md's existing links).
    live_basenames = {pkg.rsplit("/", 1)[-1] for pkg in live}
    pkg_docs_root = root / "docs" / "packages"
    if not pkg_docs_root.exists():
        return problems
    for path in sorted(pkg_docs_root.rglob("*.md")):
        rel_pkg = path.relative_to(pkg_docs_root).with_suffix("").as_posix()
        if rel_pkg in live or rel_pkg in live_basenames:
            continue
        status = package_doc_status(path.read_text())
        if status not in _PACKAGE_DOC_ALLOWED_STATUS:
            problems.append(
                f"{path.relative_to(root)}: names no live package "
                f"({rel_pkg!r}); add a 'Status: superseded' or "
                "'Status: relocated' line, or delete the page"
            )
    return problems


def _write_package_doc_fixture(root: Path) -> None:
    """_write_package_doc_fixture writes a module with one live package
    and a docs/packages tree for the check_package_doc_paths probes."""
    go_packages.write_file(root, "go.mod", f"module {go_packages.MODULE}\n\ngo 1.25.0\n")
    go_packages.write_file(root, "leaf/leaf.go", "package leaf\n\nvar Leaf = 1\n")


def _probe_live_package_doc_passes(root: Path) -> list[str]:
    _write_package_doc_fixture(root)
    go_packages.write_file(root, "docs/packages/leaf.md", "# leaf\n\nLive package.\n")
    problems = check_package_doc_paths(root)
    if problems:
        return [f"probe_live_package_doc_passes: expected pass, got {problems}"]
    return []


def _probe_stale_package_doc_without_status_fails(root: Path) -> list[str]:
    _write_package_doc_fixture(root)
    go_packages.write_file(root, "docs/packages/gone.md", "# gone\n\nNo status line.\n")
    problems = check_package_doc_paths(root)
    if not any("gone.md" in p for p in problems):
        return [
            "probe_stale_package_doc_without_status_fails: expected a "
            f"gone.md problem, got {problems}"
        ]
    return []


def _probe_stale_package_doc_superseded_passes(root: Path) -> list[str]:
    _write_package_doc_fixture(root)
    go_packages.write_file(
        root, "docs/packages/gone.md", "# gone\n\nStatus: superseded, see leaf.md\n"
    )
    problems = check_package_doc_paths(root)
    if problems:
        return [
            f"probe_stale_package_doc_superseded_passes: expected pass, got {problems}"
        ]
    return []


def run_probe() -> bool:
    """run_probe exercises the count parser against fixed strings,
    following check_plan.py's --probe convention. The go-list
    comparison itself is covered by the real tree run in make
    verify."""
    cases = {
        "six": 6,
        "ten": 10,
        "twenty": 20,
        "forty-six": 46,
        "forty-nine": 49,
        "ninety-nine": 99,
    }
    for words, want in cases.items():
        got = words_to_count(words)
        if got != want:
            print(f"words_to_count({words!r}) = {got}, want {want}")
            return False
    if words_to_count("eleven-ish") is not None:
        print("words_to_count accepted a non-count word")
        return False
    text = "Intro.\nThe diagram shows the forty-nine packages and the import edges\nbetween them.\n"
    if doc_package_count(text) != 49:
        print("doc_package_count missed the opening-paragraph phrase")
        return False
    if doc_package_count("no phrase here\n") is not None:
        print("doc_package_count invented a count from prose")
        return False
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="docs-probe-") as tmp:
        for fn in (
            _probe_live_package_doc_passes,
            _probe_stale_package_doc_without_status_fails,
            _probe_stale_package_doc_superseded_passes,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))
    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    if "--probe" in sys.argv:
        return 0 if run_probe() else 1
    violations: list[str] = []
    for path in sorted(root.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        lines = path.read_text().splitlines()
        for i, line in enumerate(lines):
            m = DECL.match(line)
            if not m:
                continue
            name = m.group(1)
            # Collect the contiguous comment block above the decl; the Go
            # convention requires its first line to start with the name.
            j = i - 1
            while j >= 0 and lines[j].strip().startswith("//"):
                j -= 1
            first = lines[j + 1].strip() if j + 1 < i else ""
            if not re.match(rf"^//\s*{re.escape(name)}\b", first):
                violations.append(f"{path.relative_to(root)}:{i + 1}: {name} lacks doc comment")
    violations.extend(check_package_count(root))
    violations.extend(check_package_doc_paths(root))
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
