#!/usr/bin/env python3
"""Process-group probe checks for scripts/check_mutation.py.
The checks live here because check_mutation.py must stay under 500
lines. See docs/plans/agents/phase75_mutation_kit_hardening.md."""
import os
import subprocess
import time
from pathlib import Path

from mutation_process import kill_group, run_test_group


PROBE_TIMEOUT_SECONDS = 0.3
PROBE_DEADLINE_SECONDS = 2.0


def _spawner_script(pid_file: Path, tail: str) -> list:
    """_spawner_script builds a shell command that backgrounds a long
    sleep, publishes that grandchild PID, and then runs tail."""
    return [
        "/bin/sh",
        "-c",
        f"sleep 120 & echo $! > {pid_file}.tmp; mv {pid_file}.tmp {pid_file}; {tail}",
    ]


def _await_pid_file(pid_file: Path):
    """_await_pid_file polls for the grandchild PID up to the deadline."""
    deadline = time.monotonic() + PROBE_DEADLINE_SECONDS
    while time.monotonic() < deadline:
        try:
            return int(pid_file.read_text().strip())
        except (OSError, ValueError):
            time.sleep(0.01)
    return None


def _is_dead_within_deadline(pid) -> bool:
    """_is_dead_within_deadline polls os.kill(pid, 0) up to the
    deadline and reports whether the process is gone."""
    if pid is None:
        return False
    deadline = time.monotonic() + PROBE_DEADLINE_SECONDS
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.01)
    return False


def probe_group_outcomes(tmp_path: Path) -> list[str]:
    """Checks: the timeout path and the success path both return their
    verdict and leave no live grandchild; a non-zero exit maps to
    "fail"."""
    problems = []
    cases = [("timeout", "sleep 120", "timeout"), ("success", "exit 0", "pass")]
    for name, tail, want in cases:
        pid_file = tmp_path / f"{name}.pid"
        got = run_test_group(
            _spawner_script(pid_file, tail), tmp_path, PROBE_TIMEOUT_SECONDS
        )
        if got != want:
            problems.append(f"{name} path: want {want!r}, got {got!r}")
        if not _is_dead_within_deadline(_await_pid_file(pid_file)):
            problems.append(f"{name} path: the grandchild process survived")
    failing = ["/bin/sh", "-c", "exit 3"]
    got = run_test_group(failing, tmp_path, PROBE_TIMEOUT_SECONDS)
    if got != "fail":
        problems.append(f"exit code mapping: want 'fail', got {got!r}")
    return problems


def probe_group_interrupt(tmp_path: Path) -> list[str]:
    """Check: a KeyboardInterrupt during the wait kills the whole group
    before it propagates. The injected wait polls for the grandchild
    PID first, so the kill cannot run before the fork."""
    problems = []
    pid_file = tmp_path / "interrupt.pid"
    seen = []

    def interrupting_wait(proc, timeout):
        seen.append(_await_pid_file(pid_file))
        raise KeyboardInterrupt

    try:
        run_test_group(
            _spawner_script(pid_file, "sleep 120"),
            tmp_path,
            PROBE_TIMEOUT_SECONDS,
            wait=interrupting_wait,
        )
        problems.append("interrupt path: KeyboardInterrupt did not propagate")
    except KeyboardInterrupt:
        pass
    if not seen or seen[0] is None:
        problems.append("interrupt path: the grandchild PID never appeared")
    elif not _is_dead_within_deadline(seen[0]):
        problems.append("interrupt path: the grandchild process survived")
    return problems


def probe_group_idempotence(tmp_path: Path) -> list[str]:
    """Check: kill_group on an already-dead group never raises."""
    proc = subprocess.Popen(["/bin/sh", "-c", "exit 0"], cwd=tmp_path, start_new_session=True)
    proc.wait()
    try:
        kill_group(proc.pid)
        kill_group(proc.pid)
    except ProcessLookupError:
        return ["kill_group: a dead group raised ProcessLookupError"]
    return []
