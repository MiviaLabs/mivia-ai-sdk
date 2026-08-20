#!/usr/bin/env python3
"""The --probe entry point for scripts/check_test_tampering.py.
Imports and runs every probe module, following check_mutation.py's
run_probe shape: each case builds its own throwaway git repo, never a
checked-in fixture."""
import tempfile
from pathlib import Path

from test_tampering_probes_diffoverride import run_diffoverride_probes
from test_tampering_probes_infra import run_infra_probes
from test_tampering_probes_testfile import run_testfile_probes
from test_tampering_probes_vectors import run_vector_probes


def run_probe() -> bool:
    """run_probe exercises every TT01-TT14 rule, the diff-resolution
    fallbacks, and the override trailers. Returns True on success."""
    problems: list = []
    with tempfile.TemporaryDirectory(prefix="test-tampering-probe-") as tmp:
        tmp_path = Path(tmp)
        problems.extend(run_testfile_probes(tmp_path))
        problems.extend(run_vector_probes(tmp_path))
        problems.extend(run_infra_probes(tmp_path))
        problems.extend(run_diffoverride_probes(tmp_path))

    if problems:
        print("\n".join(problems))
        return False
    return True
