#!/usr/bin/env python3
"""Gate: every exported top-level Go symbol needs a doc comment that
starts with the symbol name. Keeps the package surface self-describing
for AI and human consumers. Exits non-zero on violations."""
import re
import subprocess
import sys
from pathlib import Path

DECL = re.compile(r"^(?:func\s+(?:\([^)]*\)\s+)?|type\s+)([A-Z]\w*)")

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
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
