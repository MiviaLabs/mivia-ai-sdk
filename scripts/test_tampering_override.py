#!/usr/bin/env python3
"""Override-trailer resolution for scripts/check_test_tampering.py.
An `Allow-Test-Change` trailer waives one TT01-TT10/TT12/TT13 finding
with a six-word-minimum reason. An `Allow-Gate-Change` trailer is the
only way to waive TT11 or TT14, and needs fifteen words minimum. No
env var or CLI flag ever waives a finding; only a permanent, attributable
commit-message trailer does. See docs/plans/test-tampering.md, "Override
mechanism"."""
import re
from dataclasses import dataclass

TEST_CHANGE_IDS = frozenset({f"TT{n:02d}" for n in list(range(1, 11)) + [12, 13]})
GATE_CHANGE_IDS = frozenset({"TT11", "TT14"})

_BOILERPLATE = {
    "fix", "cleanup", "refactor", "wip", "misc", "temp", "n/a", "na", "ok", "done",
}
_TRAILER_RE = re.compile(r"^(Allow-Test-Change|Allow-Gate-Change):\s*(TT\d{2})[,:]?\s*(.*)$")

_TEST_CHANGE_MIN_WORDS = 6
_GATE_CHANGE_MIN_WORDS = 15


@dataclass
class Trailer:
    """Trailer is one parsed override line: which name, which finding
    ID, its reason text, and the raw line for the informational print."""

    kind: str  # "test" or "gate"
    id: str
    reason: str
    raw_line: str


def _significant_word_count(reason: str) -> int:
    """_significant_word_count strips boilerplate filler words before
    counting; a reason of only filler words, or only punctuation with
    no letters, counts as zero."""
    words = reason.split()
    stripped = [w.strip(".,;:!?").lower() for w in words]
    significant = [w for w in stripped if w and w not in _BOILERPLATE]
    return len(significant)


def parse_trailers(message: str) -> list:
    """parse_trailers scans a commit message for override trailer
    lines. A malformed line (wrong prefix) is simply not a trailer;
    shape validity is checked later, per finding, in resolve_overrides."""
    if not message:
        return []
    trailers = []
    for line in message.splitlines():
        m = _TRAILER_RE.match(line.strip())
        if not m:
            continue
        kind_name, tid, reason = m.groups()
        kind = "test" if kind_name == "Allow-Test-Change" else "gate"
        trailers.append(Trailer(kind, tid, reason.strip(), line.strip()))
    return trailers


def _valid_trailer(finding_id: str, trailer: "Trailer") -> bool:
    """_valid_trailer enforces the two-tier bar: TT11/TT14 need a gate
    trailer with 15+ significant words; every other ID needs a test
    trailer with 6+. A wrong-kind or short-reason trailer is invalid,
    the same as no trailer at all."""
    if finding_id in GATE_CHANGE_IDS:
        return trailer.kind == "gate" and _significant_word_count(trailer.reason) >= _GATE_CHANGE_MIN_WORDS
    if finding_id in TEST_CHANGE_IDS:
        return trailer.kind == "test" and _significant_word_count(trailer.reason) >= _TEST_CHANGE_MIN_WORDS
    return False


def resolve_overrides(findings: list, message: str):
    """resolve_overrides splits findings into (unresolved, overridden)
    using the message's trailers. The first trailer per ID wins.

    A non-gate finding (TT01-TT10, TT12, TT13) resolves on its own
    valid Allow-Test-Change trailer. A gate finding (TT11, TT14) needs
    its own valid Allow-Gate-Change trailer, but that trailer never
    covers a sibling by itself: if any non-gate finding in the same
    diff is still unresolved, every gate finding stays unresolved too,
    even one with an otherwise-valid trailer of its own."""
    trailers = parse_trailers(message)
    first_by_id: dict = {}
    for t in trailers:
        first_by_id.setdefault(t.id, t)

    def resolved(f):
        t = first_by_id.get(f.id)
        return t if t is not None and _valid_trailer(f.id, t) else None

    non_gate = [f for f in findings if f.id not in GATE_CHANGE_IDS]
    gate = [f for f in findings if f.id in GATE_CHANGE_IDS]

    unresolved = []
    overridden = []
    non_gate_all_resolved = True
    for f in non_gate:
        t = resolved(f)
        if t is not None:
            overridden.append((f, t))
        else:
            unresolved.append(f)
            non_gate_all_resolved = False

    for f in gate:
        t = resolved(f)
        if t is not None and non_gate_all_resolved:
            overridden.append((f, t))
        else:
            unresolved.append(f)
    return unresolved, overridden
