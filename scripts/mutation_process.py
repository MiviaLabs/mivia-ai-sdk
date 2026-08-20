#!/usr/bin/env python3
"""Process-group helpers for scripts/check_mutation.py.
A `go test` run spawns a compiled test binary as a grandchild. Killing
the direct child alone leaks that binary, so every path here kills the
whole process group. POSIX only; stdlib only."""
import os
import signal
import subprocess

from mutation_tokenize import MutationError


def kill_group(pid: int) -> None:
    """kill_group sends SIGKILL to the whole group led by pid.
    Idempotent: a dead group raises ProcessLookupError, which is
    swallowed, so a second call is a no-op."""
    if not hasattr(os, "killpg"):
        raise MutationError("mutation kit needs os.killpg; POSIX is the supported platform")
    try:
        os.killpg(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def _default_wait(proc, timeout):
    """_default_wait waits for proc up to timeout seconds."""
    return proc.wait(timeout)


def run_test_group(argv: list, cwd, timeout: float, wait=None) -> str:
    """run_test_group runs argv in its own process group and returns
    "pass", "fail", or "timeout". The group is killed on timeout, on
    normal completion, and on KeyboardInterrupt before the re-raise, so
    no test binary outlives the call. wait(proc, timeout) exists for
    the probe; production callers leave it None."""
    if not hasattr(os, "killpg"):
        raise MutationError("mutation kit needs os.killpg; POSIX is the supported platform")
    if wait is None:
        wait = _default_wait
    proc = subprocess.Popen(
        argv,
        cwd=cwd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    try:
        try:
            code = wait(proc, timeout)
        except subprocess.TimeoutExpired:
            kill_group(proc.pid)
            proc.wait()
            return "timeout"
        kill_group(proc.pid)
        return "pass" if code == 0 else "fail"
    finally:
        kill_group(proc.pid)
