#!/usr/bin/env python3
"""Internal-test-target probe checks for scripts/check_mutation.py.
The checks live here because check_mutation.py must stay under 500
lines, the precedent scripts/mutation_process.py set. Every check runs
against a throwaway Go module in a temp directory, never against the
repo tree. See docs/plans/mutation.md."""
import shutil
import subprocess
from pathlib import Path

from mutation_process import run_test_group
from mutation_process_probe import _await_pid_file, _is_dead_within_deadline
from mutation_tokenize import MutationError, sites_for_file

# FIXTURE_TIMEOUT_SECONDS bounds a fixture run that must finish.
FIXTURE_TIMEOUT_SECONDS = 60.0

# HANG_TIMEOUT_SECONDS bounds the check whose fixture hangs on purpose.
# It must exceed the measured cold-cache time for a two-target `go
# test` to reach a test body, which is 1.93 s. The check pre-builds the
# module, so the real wait is this constant. Do not reuse
# mutation_process_probe.PROBE_TIMEOUT_SECONDS: at 0.3 s the group dies
# before either test binary writes its PID file, and the check then
# reports a false survivor. See docs/plans/mutation.md.
HANG_TIMEOUT_SECONDS = 5.0

PKG = "probepkg"
EXT_DIR = f"{PKG}/{PKG}_test"
PKG_TARGET = f"./{PKG}"
EXT_TARGET = f"./{EXT_DIR}"

GO_MOD = "module probemod\n\ngo 1.25.0\n"

SOURCE = """package probepkg

// F reports whether n is at least 8.
func F(n int) bool {
	return n >= 8
}

// G reports whether n is below 4.
func G(n int) bool {
	return n < 4
}
"""

INTERNAL_TEST = """package probepkg

import "testing"

func TestF(t *testing.T) {
	if !F(8) {
		t.Fatal("F(8) must be true")
	}
}
"""

EXTERNAL_TEST = """package probepkg_test

import (
	"testing"

	"probemod/probepkg"
)

func TestG(t *testing.T) {
	if probepkg.G(4) {
		t.Fatal("G(4) must be false")
	}
}
"""

SLEEP_TEST = """package {pkg}

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func {name}(t *testing.T) {{
	path := "{pid_file}"
	_ = os.WriteFile(path+".part", []byte(strconv.Itoa(os.Getpid())), 0o600)
	_ = os.Rename(path+".part", path)
	time.Sleep(120 * time.Second)
}}
"""


def write_fixture(root: Path) -> None:
    """write_fixture builds the throwaway Go module under root.

    F owns a `>=` site that only the internal test kills. G owns a `<`
    site that only the external test kills.
    """
    (root / EXT_DIR).mkdir(parents=True)
    (root / "go.mod").write_text(GO_MOD)
    (root / PKG / f"{PKG}.go").write_text(SOURCE)
    (root / PKG / "internal_test.go").write_text(INTERNAL_TEST)
    (root / EXT_DIR / "ext_test.go").write_text(EXTERNAL_TEST)


def copy_fixture(source: Path, target: Path) -> Path:
    """copy_fixture clones a built fixture so one check cannot disturb
    another. Returns target."""
    shutil.copytree(source, target)
    return target


def site_of(root: Path, kind: str):
    """site_of returns the single mutation site of the named kind in the
    fixture source. Raises when the count is not one."""
    path = root / PKG / f"{PKG}.go"
    found = [s for s in sites_for_file(path) if s.kind == kind]
    if len(found) != 1:
        raise MutationError(f"fixture: want 1 {kind!r} site, got {len(found)}")
    return found[0]


def run_mutant_here(kit, root: Path, kind: str) -> str:
    """run_mutant_here scores one fixture site through the kit's own
    run_mutant, with root set to the throwaway module."""
    site = site_of(root, kind)
    original = site.path.read_bytes()
    return kit.run_mutant(site, original, PKG, str(root / PKG), root=root)


def check_target_selection(kit, root: Path, tmp_path: Path) -> list[str]:
    """Checks 3, 4 and 5: both paths when the external directory holds a
    .go file, the package path alone otherwise."""
    problems = []
    got = kit.test_targets(root / PKG, PKG)
    if got != [PKG_TARGET, EXT_TARGET]:
        problems.append(f"test_targets: want both paths, package first, got {got}")

    bare = tmp_path / "bare" / PKG
    bare.mkdir(parents=True)
    got = kit.test_targets(bare, PKG)
    if got != [PKG_TARGET]:
        problems.append(f"test_targets: want the package path alone, got {got}")

    hollow = tmp_path / "hollow" / PKG
    (hollow / f"{PKG}_test").mkdir(parents=True)
    (hollow / f"{PKG}_test" / "notes.txt").write_text("no go file here\n")
    got = kit.test_targets(hollow, PKG)
    if got != [PKG_TARGET]:
        problems.append(f"test_targets: an empty external dir added a path: {got}")
    return problems


def check_internal_test_kills(kit, root: Path) -> list[str]:
    """Checks 1 and 2: the internal test kills the `>=` mutant, and the
    external target alone passes under that same mutant."""
    problems = []
    if run_mutant_here(kit, root, ">=") != kit.KILLED:
        problems.append("run_mutant: the internal test did not kill the '>=' mutant")

    site = site_of(root, ">=")
    original = site.path.read_bytes()
    text = original.decode("utf-8")
    site.path.write_text(text[: site.start] + site.new + text[site.end :])
    try:
        alone = run_test_group(
            ["go", "test", EXT_TARGET], root, FIXTURE_TIMEOUT_SECONDS
        )
    finally:
        site.path.write_bytes(original)
    if alone != "pass":
        problems.append(f"fixture: the external target alone must pass, got {alone!r}")
    return problems


def check_external_test_kills(kit, root: Path) -> list[str]:
    """Check 7: the external test kills the `<` mutant, so the rule
    holds in both directions."""
    if run_mutant_here(kit, root, "<") != kit.KILLED:
        return ["run_mutant: the external test did not kill the '<' mutant"]
    return []


def check_empty_package_target(kit, root: Path) -> list[str]:
    """Check 8: a target with no test file never fabricates a failure
    and never hides one. This fixture has no internal test file."""
    problems = []
    (root / PKG / "internal_test.go").unlink()
    if run_mutant_here(kit, root, "<") != kit.KILLED:
        problems.append("run_mutant: an empty package target hid a real failure")
    if run_mutant_here(kit, root, ">=") != kit.SURVIVED:
        problems.append("run_mutant: an empty package target fabricated a failure")
    return problems


def _add_sleeping_tests(root: Path) -> tuple[Path, Path]:
    """_add_sleeping_tests plants one hanging test in each target. Each
    test publishes its own PID before it sleeps."""
    internal_pid = root / "internal.pid"
    external_pid = root / "external.pid"
    (root / PKG / "sleep_internal_test.go").write_text(
        SLEEP_TEST.format(pkg=PKG, name="TestSleepInternal", pid_file=internal_pid)
    )
    (root / EXT_DIR / "sleep_ext_test.go").write_text(
        SLEEP_TEST.format(
            pkg=f"{PKG}_test", name="TestSleepExternal", pid_file=external_pid
        )
    )
    return internal_pid, external_pid


def check_group_timeout(root: Path) -> list[str]:
    """Check 6: a two-target `go test` that hangs returns "timeout" and
    leaves no live process in the group."""
    problems = []
    internal_pid, external_pid = _add_sleeping_tests(root)
    subprocess.run(
        ["go", "test", "-run=NONE", "-count=1", PKG_TARGET, EXT_TARGET],
        cwd=root,
        capture_output=True,
        text=True,
    )
    argv = ["go", "test", PKG_TARGET, EXT_TARGET]
    got = run_test_group(argv, root, HANG_TIMEOUT_SECONDS)
    if got != "timeout":
        problems.append(f"two-target timeout: want 'timeout', got {got!r}")
    for name, pid_file in (("internal", internal_pid), ("external", external_pid)):
        if not _is_dead_within_deadline(_await_pid_file(pid_file)):
            problems.append(f"two-target timeout: the {name} test process survived")
    return problems


def probe_internal_test_target(tmp_path: Path, kit) -> list[str]:
    """probe_internal_test_target runs every internal-test-target check
    against throwaway Go modules. kit is the check_mutation module."""
    root = tmp_path / "targetmod"
    root.mkdir()
    write_fixture(root)
    problems = check_target_selection(kit, root, tmp_path)
    problems += check_internal_test_kills(kit, root)
    problems += check_external_test_kills(kit, root)
    problems += check_group_timeout(copy_fixture(root, tmp_path / "hangmod"))
    problems += check_empty_package_target(kit, copy_fixture(root, tmp_path / "baremod"))
    return problems
