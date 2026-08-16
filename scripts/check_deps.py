#!/usr/bin/env python3
"""Gate: internal imports must follow policy/layers.json. A package may
import only the internal packages the policy lists; a package missing
from the policy fails. Test files are exempt (integration tests cross
layers on purpose)."""
import json
import re
import sys
from pathlib import Path

MODULE = "github.com/MiviaLabs/mivia-ai-sdk"
IMPORT = re.compile(r'"(?:' + re.escape(MODULE) + r')/([a-z0-9_-]+)"')


def package_dirs(root: Path) -> list[str]:
    out = []
    for d in sorted(root.iterdir()):
        if d.is_dir() and not d.name.startswith(".") and d.name != "scripts":
            if any(d.glob("*.go")):
                out.append(d.name)
    return out


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    policy = json.loads((root / "policy" / "layers.json").read_text())["allowed_imports"]
    problems = []
    for pkg in package_dirs(root):
        if pkg not in policy:
            problems.append(f"{pkg}: missing from policy/layers.json; declare its allowed imports first")
            continue
        allowed = set(policy[pkg])
        for src in sorted((root / pkg).glob("*.go")):
            if src.name.endswith("_test.go"):
                continue
            for imp in IMPORT.findall(src.read_text()):
                if imp not in allowed:
                    problems.append(
                        f"{pkg}: imports {imp}, not allowed by policy/layers.json "
                        f"(allowed: {sorted(allowed) or 'none'})"
                    )
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
