#!/usr/bin/env python3
"""Gate/tool: mutation testing for one package at a time.
Applies text-level operator mutations to a package's tracked source,
runs that package's own test target per mutant, and reports the kill
rate against a per-package floor. See
docs/plans/agents/phase54_mutation_kit.md. Stdlib-only plus the go
tool: site-finding shells out to a small embedded go/scanner helper
program, run through `go run`, never a third-party mutation library.
Tokenizer glue lives in scripts/mutation_tokenize.py."""
import argparse
import json
import signal
import subprocess
import sys
import tempfile
from pathlib import Path

from mutation_process import run_test_group
from mutation_process_probe import (
    probe_group_idempotence,
    probe_group_interrupt,
    probe_group_outcomes,
)
from mutation_target_probe import probe_internal_test_target
from mutation_tokenize import MutationError, sites_for_file, sites_from_tokens

# Python's default SIGTERM handling kills the process without running
# `finally` blocks, unlike SIGINT. A killed sweep would then leave a
# mutated file on disk. Re-raising SIGTERM as KeyboardInterrupt routes
# it through the same finally-guaranteed restore path as Ctrl-C.
signal.signal(signal.SIGTERM, signal.default_int_handler)

ROOT = Path(__file__).resolve().parent.parent
DENYLIST_DIR = ROOT / "scripts" / "mutation_denylist"
TEST_TIMEOUT_SECONDS = 60

KILLED = "killed"
SURVIVED = "survived"
DISCARDED = "discarded"


def load_denylist(pkg: str, denylist_dir: Path = DENYLIST_DIR) -> dict:
    """load_denylist reads a package's denylist and floor, or empty defaults."""
    path = denylist_dir / f"{pkg}.json"
    if not path.exists():
        return {"denylist": [], "floor": None}
    data = json.loads(path.read_text())
    data.setdefault("denylist", [])
    data.setdefault("floor", None)
    return data


def denylisted_spans(pkg_dir: Path, denylist: list[dict]) -> dict:
    """denylisted_spans resolves each denylist entry to its one exact
    match span in its named file. Fails loudly on zero or on more
    than one match; a stale or ambiguous entry never rubber-stamps a
    site silently."""
    spans: dict[Path, list[tuple[int, int]]] = {}
    for entry in denylist:
        file_path = pkg_dir / entry["file"]
        text = file_path.read_text()
        snippet = entry["snippet"]
        count = text.count(snippet)
        if count == 0:
            raise MutationError(
                f"denylist entry for {entry['file']}: snippet no longer matches: {snippet!r}"
            )
        if count > 1:
            raise MutationError(
                f"denylist entry for {entry['file']}: snippet matches {count} sites, "
                f"widen it to match one: {snippet!r}"
            )
        start = text.index(snippet)
        spans.setdefault(file_path, []).append((start, start + len(snippet)))
    return spans


def is_denylisted(site, spans: dict) -> bool:
    """is_denylisted reports whether site falls inside a denylisted span."""
    for start, end in spans.get(site.path, []):
        if site.start >= start and site.end <= end:
            return True
    return False


def is_build_tag_gated(path: Path) -> bool:
    """is_build_tag_gated reports a `//go:build` constraint on path.

    A tag-gated file (for example ledger's ledger_sqlite files) never
    compiles into the default build the kit tests against, so mutating
    it would always "survive" without meaning anything. The kit
    excludes it, mirroring how the Makefile's default (untagged)
    coverage block already excludes ledger_sqlite-tagged code.
    """
    for line in path.read_text().splitlines():
        stripped = line.strip()
        if stripped == "":
            continue
        return stripped.startswith("//go:build")
    return False


def source_files(pkg_dir: Path) -> list[Path]:
    """source_files lists a package's mutable files: tracked, non-test,
    and not gated behind a build tag."""
    return sorted(
        p
        for p in pkg_dir.glob("*.go")
        if not p.name.endswith("_test.go") and not is_build_tag_gated(p)
    )


def collect_sites(pkg: str, denylist_dir: Path = DENYLIST_DIR) -> list:
    """collect_sites returns pkg's deterministic mutant list: sorted by
    file, then by byte offset, with denylisted sites already removed."""
    pkg_dir = ROOT / pkg
    data = load_denylist(pkg, denylist_dir)
    spans = denylisted_spans(pkg_dir, data["denylist"])
    sites = []
    for path in source_files(pkg_dir):
        for site in sites_for_file(path):
            if not is_denylisted(site, spans):
                sites.append(site)
    sites.sort(key=lambda s: s.sort_key())
    return sites


def test_targets(pkg_dir: Path, pkg: str) -> list[str]:
    """test_targets lists the go test paths for pkg: the package
    directory first, then its external test directory when that
    directory exists and holds a .go file. Both paths go to one `go
    test` run, which fails when either package fails. The package
    directory is never checked for a test file: `go test` on a
    directory with none exits 0."""
    targets = [f"./{pkg}"]
    ext = pkg_dir / f"{pkg}_test"
    if ext.is_dir() and any(ext.glob("*.go")):
        targets.append(f"./{pkg}/{pkg}_test")
    return targets


def classify(build_ok: bool, test_outcome: str) -> str:
    """classify maps one mutant's build result and test outcome to a
    verdict. test_outcome is "pass", "fail", or "timeout"."""
    if not build_ok:
        return DISCARDED
    if test_outcome in ("fail", "timeout"):
        return KILLED
    return SURVIVED


def run_mutant(site, original: bytes, pkg: str, pkg_dir: str, root: Path = ROOT) -> str:
    """run_mutant applies one mutation, builds, tests, and restores the
    original bytes no matter how the run ends. root sets the working
    directory for go; only the probe passes a value other than ROOT."""
    text = original.decode("utf-8")
    mutated = text[: site.start] + site.new + text[site.end :]
    site.path.write_text(mutated)
    try:
        build = subprocess.run(
            ["go", "build", f"./{pkg}"], cwd=root, capture_output=True, text=True
        )
        if build.returncode != 0:
            print(f"discarded (build failed): {site.path.name}:{site.start} {site.kind}")
            return classify(False, "pass")
        targets = test_targets(Path(pkg_dir), pkg)
        outcome = run_test_group(["go", "test", *targets], root, TEST_TIMEOUT_SECONDS)
        return classify(True, outcome)
    finally:
        site.path.write_bytes(original)


def sweep(pkg: str, sample: int = None, denylist_dir: Path = DENYLIST_DIR) -> dict:
    """sweep runs every mutant (or the first sample) for pkg and
    returns the kill counts and rate. Every mutated file's original
    bytes are restored, including on interrupt or crash, through the
    finally block below."""
    pkg_dir = ROOT / pkg
    sites = collect_sites(pkg, denylist_dir)
    if sample is not None:
        sites = sites[:sample]
    originals: dict[Path, bytes] = {}
    killed = survived = discarded = 0
    try:
        for index, site in enumerate(sites, start=1):
            if site.path not in originals:
                originals[site.path] = site.path.read_bytes()
            outcome = run_mutant(site, originals[site.path], pkg, str(pkg_dir))
            if outcome == KILLED:
                killed += 1
            elif outcome == SURVIVED:
                survived += 1
                print(
                    f"SURVIVED: {site.path.name}:{site.start} "
                    f"{site.kind} {site.old!r} -> {site.new!r}"
                )
            else:
                discarded += 1
            print(
                f"[{index}/{len(sites)}] {site.path.name}:{site.start} {outcome}",
                file=sys.stderr,
                flush=True,
            )
    finally:
        for path, original in originals.items():
            path.write_bytes(original)
    total = killed + survived
    rate = 100.0 * killed / total if total else 100.0
    return {"killed": killed, "survived": survived, "discarded": discarded, "rate": rate}


def resolve_floor(pkg: str, cli_floor, denylist_dir: Path = DENYLIST_DIR):
    """resolve_floor honors a CLI --floor for one run only; otherwise it
    reads the package's own stored floor, or None if it has none yet."""
    if cli_floor is not None:
        return cli_floor
    return load_denylist(pkg, denylist_dir).get("floor")


def recognized_packages() -> set:
    """recognized_packages lists top-level dirs holding non-test .go
    files, the same discovery rule scripts/check_plan.py uses."""
    names = set()
    for d in ROOT.iterdir():
        if not d.is_dir() or d.name.startswith(".") or d.name == "scripts":
            continue
        for f in d.glob("*.go"):
            if not f.name.endswith("_test.go"):
                names.add(d.name)
                break
    return names


def _probe_planted_site(pkg_dir: Path) -> list[str]:
    """Check: the planted `==` site is generated, and the comment and
    string-literal occurrences of "==" are excluded."""
    planted = pkg_dir / "planted.go"
    planted.write_text(
        'package probepkg\n\n'
        '// a == b in a comment must never become a site\n'
        'const s = "a == b"\n\n'
        'func f(a, b int) bool {\n'
        '\tif a == b {\n'
        '\t\treturn true\n'
        '\t}\n'
        '\treturn false\n'
        '}\n'
    )
    eq_sites = [s for s in sites_for_file(planted) if s.kind == "=="]
    if len(eq_sites) != 1:
        return [f"planted site: want 1 '==' candidate, got {len(eq_sites)}"]
    return []


def _probe_build_tag_exclusion(pkg_dir: Path) -> list[str]:
    """Check: a `//go:build` gated file is excluded from source_files,
    even though it holds an obvious mutation candidate."""
    tagged = pkg_dir / "tagged.go"
    tagged.write_text(
        '//go:build sometag\n\n'
        'package probepkg\n\n'
        'func h(a, b int) bool {\n'
        '\treturn a == b\n'
        '}\n'
    )
    if tagged in source_files(pkg_dir):
        return ["build-tag exclusion: a //go:build file was not excluded"]
    return []


def _probe_denylist(pkg_dir: Path) -> list[str]:
    """Checks: a denylisted site is skipped once its snippet is loaded;
    a missing or ambiguous snippet fails loudly instead of silently
    matching zero or more than one site."""
    problems = []
    denylisted = pkg_dir / "denylisted.go"
    denylisted.write_text(
        'package probepkg\n\n'
        'func g(x, y int) bool {\n'
        '\treturn x == y\n'
        '}\n'
    )
    entries = [{"file": "denylisted.go", "snippet": "x == y"}]
    spans = denylisted_spans(pkg_dir, entries)
    remaining = [s for s in sites_for_file(denylisted) if not is_denylisted(s, spans)]
    if remaining:
        problems.append("denylisted site: still present after filtering")

    try:
        denylisted_spans(pkg_dir, [{"file": "denylisted.go", "snippet": "no such text"}])
        problems.append("denylist: missing snippet did not raise")
    except MutationError:
        pass

    ambiguous = pkg_dir / "ambiguous.go"
    ambiguous.write_text(
        'package probepkg\n\n'
        'func h(a, b int) bool {\n'
        '\t_ = a == b\n'
        '\t_ = a == b\n'
        '\treturn true\n'
        '}\n'
    )
    try:
        denylisted_spans(ambiguous.parent, [{"file": "ambiguous.go", "snippet": "a == b"}])
        problems.append("denylist: ambiguous snippet did not raise")
    except MutationError:
        pass
    return problems


def _probe_not_guard(pkg_dir: Path) -> list[str]:
    """Check: a `!` token immediately followed by `=` is never a
    candidate. Real go/scanner output never produces this adjacency
    (it merges `!=` into one token); this exercises the defensive
    guard itself with a synthetic token list."""
    problems = []
    placeholder = pkg_dir / "planted.go"
    synthetic = [{"pos": 0, "end": 1, "tok": "!"}]
    guarded = sites_from_tokens(synthetic, b"!=", placeholder)
    if guarded:
        problems.append("dropped-! guard: '!' immediately before '=' produced a site")
    unguarded = sites_from_tokens(synthetic, b"! ", placeholder)
    if not unguarded:
        problems.append("dropped-! guard: '!' not before '=' produced no site")
    return problems


def _probe_classify() -> list[str]:
    """Checks: a failed build is discarded; a timeout or a failing test
    counts as killed; a passing test counts as survived."""
    problems = []
    if classify(False, "pass") != DISCARDED:
        problems.append("classify: a failed build must discard the mutant")
    if classify(True, "timeout") != KILLED:
        problems.append("classify: a timed-out test run must count as killed")
    if classify(True, "fail") != KILLED:
        problems.append("classify: a failing test run must count as killed")
    if classify(True, "pass") != SURVIVED:
        problems.append("classify: a passing test run must count as survived")
    return problems


def _probe_internal_test_target(tmp_path: Path) -> list[str]:
    """Checks: a mutant that only a package's own internal test kills is
    scored killed; target selection lists both paths, package directory
    first; a timed-out two-target run leaves no live process. See
    scripts/mutation_target_probe.py."""
    return probe_internal_test_target(tmp_path, sys.modules[__name__])


def _probe_floor(tmp_path: Path) -> list[str]:
    """Check: the floor comes from the package's own JSON file when
    --floor is absent; --floor on the CLI overrides it for that run
    only and never writes the file."""
    problems = []
    denylist_dir = tmp_path / "mutation_denylist"
    denylist_dir.mkdir()
    (denylist_dir / "probepkg.json").write_text(json.dumps({"denylist": [], "floor": 77}))
    if resolve_floor("probepkg", None, denylist_dir) != 77:
        problems.append("resolve_floor: did not read the package's stored floor")
    if resolve_floor("probepkg", 90, denylist_dir) != 90:
        problems.append("resolve_floor: a CLI floor did not override the stored floor")
    if load_denylist("probepkg", denylist_dir)["floor"] != 77:
        problems.append("resolve_floor: a CLI override must not write the package file")
    return problems


def _probe_process_group(tmp_path: Path) -> list[str]:
    """Checks: the process group is killed on the timeout path, the
    success path, and the interrupt path; kill_group is idempotent."""
    return (
        probe_group_outcomes(tmp_path)
        + probe_group_interrupt(tmp_path)
        + probe_group_idempotence(tmp_path)
    )


def run_probe() -> bool:
    """run_probe exercises the kit's own invariants against planted
    fixtures, following the check_semgrep_probes.py convention: no
    fixture is a checked-in .go file. Returns True on success."""
    problems = []
    with tempfile.TemporaryDirectory(prefix="mutation-probe-") as tmp:
        tmp_path = Path(tmp)
        pkg_dir = tmp_path / "probepkg"
        pkg_dir.mkdir()
        problems += _probe_planted_site(pkg_dir)
        problems += _probe_build_tag_exclusion(pkg_dir)
        problems += _probe_denylist(pkg_dir)
        problems += _probe_not_guard(pkg_dir)
        problems += _probe_classify()
        problems += _probe_internal_test_target(tmp_path)
        problems += _probe_floor(tmp_path)
        problems += _probe_process_group(tmp_path)

    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="mutation kit: per-package kill rate")
    parser.add_argument("--pkg", help="package directory to mutate")
    parser.add_argument("--floor", type=float, help="override the stored floor for this run")
    parser.add_argument("--sample", type=int, help="run only the first N mutants")
    parser.add_argument("--probe", action="store_true", help="run the kit's own probe suite")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    if not args.pkg:
        print("--pkg is required unless --probe is set")
        return 2
    if args.pkg not in recognized_packages():
        print(f"unrecognized package: {args.pkg}")
        return 2

    try:
        result = sweep(args.pkg, args.sample)
    except MutationError as exc:
        print(str(exc))
        return 1

    floor = resolve_floor(args.pkg, args.floor)
    print(
        f"{args.pkg}: killed={result['killed']} survived={result['survived']} "
        f"discarded={result['discarded']} rate={result['rate']:.2f}%"
    )
    if floor is None:
        print(f"{args.pkg}: no floor set on the CLI or in its denylist file; exploratory run")
        return 0
    if result["rate"] < floor:
        print(f"{args.pkg}: kill rate {result['rate']:.2f}% below the {floor}% floor")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
