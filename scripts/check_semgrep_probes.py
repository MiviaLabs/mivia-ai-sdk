#!/usr/bin/env python3
"""Gate: every Semgrep rule fires on its violation and stays silent on
its clean counterpart. The probe writes snippets to a temp dir, runs
Semgrep once with the repo config, and asserts the expected rule IDs.
The suppression probe runs the suppression grep command on a marker fixture."""
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CONFIG = str(ROOT / "semgrep")
GREP_PATTERN = "(//|#)\\s*nose" + "m" + "grep"

# Marker words are built from fragments. The suppression grep and the drift scan
# read this file; a literal marker here would trip the gate it guards.
SUPPRESS = "nose" + "mgrep"
D_MARK = "TO" + "DO"
H_MARK = "HA" + "CK"
SPACED_D = "T O " + "D O"

PROBES = [
    (
        "sdk.go.no-panic-in-packages",
        "viol_no_panic.go",
        'package p\n\nfunc f() {\n\tpanic("boom")\n}\n',
        "clean_no_panic.go",
        "package p\n\nfunc f() {\n\t_ = 1\n}\n",
    ),
    (
        "sdk.go.no-process-exit-in-packages",
        "viol_process_exit.go",
        'package p\n\nimport "log"\n\nfunc f() {\n\tlog.Fatal("a")\n\tlog.Fatalf("b")\n\tlog.Fatalln("c")\n\tlog.Panic("d")\n\tlog.Panicf("e")\n\tlog.Panicln("g")\n\tos.Exit(1)\n}\n',
        "clean_process_exit.go",
        'package p\n\nimport "log"\n\nfunc f() {\n\tlog.Print("ok")\n\t_ = 1\n}\n',
    ),
    (
        "sdk.go.stdlib-only-imports",
        "viol_import.go",
        'package p\n\nimport "GitHub.com/acme/lib"\n',
        "clean_import.go",
        'package p\n\nimport (\n\t"encoding/json"\n\t"fmt"\n)\n',
    ),
    (
        "sdk.go.no-enum-string-literals",
        "viol_enum.go",
        'package p\n\ntype Intent string\n\ntype ExecutionClass string\n\nfunc f() {\n\t_ = Intent("x")\n\t_ = Intent( "y")\n\t_ = Intent(`z`)\n\tm := struct{ Intent string }{Intent: "w"}\n\t_ = m\n\t_ = ExecutionClass("x")\n}\n',
        "clean_enum.go",
        'package p\n\nconst IntentRequest = "request"\n\nconst ExecutionClassRead ExecutionClass = "read"\n\ntype ExecutionClass string\n\nfunc f() {\n\tm := struct{ Intent string }{}\n\t_ = m\n\t_ = ExecutionClassRead\n}\n',
    ),
    (
        "sdk.go.hash-prefix-centralized",
        "viol_hash.go",
        'package p\n\nfunc f() {\n\ta := "sha256:" + "x"\n\tb := `sha256:` + "x"\n\tc := "sha256" + ":" + "x"\n\t_, _, _ = a, b, c\n}\n',
        "clean_hash.go",
        'package p\n\nconst HashPrefix = "sha256:"\n\nfunc f() {\n\t_ = HashPrefix\n}\n',
    ),
    (
        "sdk.go.marshal-via-encode",
        "viol_marshal.go",
        'package p\n\nimport "encoding/json"\n\nfunc f() {\n\t_ = json.Marshal(x)\n\t_ = json.MarshalIndent(x, "", "  ")\n\tvar w interface{}\n\t_ = json.NewEncoder(w).Encode(x)\n}\n',
        "clean_marshal.go",
        "package p\n\nfunc f() {\n\t_ = 1\n}\n",
    ),
    (
        "sdk.go.sign-via-sign",
        "viol_sign.go",
        'package p\n\nimport "crypto"\n\nfunc f(key ed25519.PrivateKey, msg []byte) {\n\t_ = ed25519.Sign(key, msg, key)\n\t_ = key.Sign(nil, msg, crypto.Hash(0))\n}\n',
        "clean_sign.go",
        "package p\n\nfunc f(key ed25519.PrivateKey, msg []byte) {\n\t_ = envelope.Sign(key, msg)\n}\n",
    ),
    (
        "sdk.go.no-reflection-in-packages",
        "viol_reflect.go",
        'package p\n\nimport "reflect"\n\nfunc f(x interface{}) interface{} {\n\tt := reflect.TypeOf(x)\n\treturn reflect.New(t)\n}\n',
        "clean_reflect.go",
        "package p\n\nfunc f(x string) string {\n\treturn table[x]\n}\n",
    ),
    (
        "sdk.go.no-hardcoded-secrets",
        "viol_secrets.go",
        'package p\n\nfunc f() {\n\ttoken := "abcdefghij"\n\tsecret := "abcdefghij"\n\tapiKey := "abcdefghij"\n\traw := `abcdefghij`\n\t_, _, _, _ = token, secret, apiKey, raw\n}\n',
        "clean_secrets.go",
        'package p\n\nfunc f() {\n\tkey := "abcdefghij"\n\t_ = key\n\t_ = os.Getenv("TOKEN")\n}\n',
    ),
    (
        "sdk.go.tests-no-time-sleep",
        "viol_test_sleep_test.go",
        'package p\n\nimport "time"\n\nfunc TestSleep(t *testing.T) {\n\ttime.Sleep(time.Second)\n}\n',
        "clean_test_sleep_test.go",
        "package p\n\nfunc TestOk(t *testing.T) {\n\t_ = 1\n}\n",
    ),
    (
        "sdk.generic.no-semgrep-suppression",
        "viol_suppression.txt",
        SUPPRESS + "\n",
        "clean_suppression.txt",
        'x := "' + SUPPRESS + '"\n',
    ),
    (
        "sdk.generic.no-drift-markers",
        "viol_drift.md",
        "# Title\n\n" + D_MARK + ": finish this\n\n" + SPACED_D + "\n\n" + H_MARK + " alert\n",
        "clean_drift.md",
        "# Title\n\nEverything is done.\n\nwith AckRequired set the check runs\n",
    ),
    (
        "sdk.go.no-phase-tdd-perf-names",
        "viol_bad_names.go",
        'package p\n\nfunc phase07Build() {}\nfunc testTDDHelper() {}\nfunc perfMeasure() {}\nfunc wipHandler() {}\nfunc draft_v2() {}\n',
        "clean_good_names.go",
        'package p\n\nfunc panelBuild() {}\nfunc testHelper() {}\nfunc allocMeasure() {}\nfunc parseIPv4() {}\n',
    ),
    (
        "sdk.go.stdlib-only-imports",
        "viol_other_import_outside.go",
        'package p\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n',
        "clean_other_import_outside.go",
        'package p\n\nimport "encoding/json"\n\nvar _ = json.RawMessage(nil)\n',
    ),
    # Proves the blanket rule's global pattern-not-regex module
    # allowances: silent on an allowed module path, firing on another
    # domain-shaped import in the same directory. The five allowances
    # replace the former per-directory exclude list.
    (
        "sdk.go.stdlib-only-imports",
        "viol_domain_shaped_import.go",
        'package p\n\nimport "example.com/other/pkg"\n\nvar _ = pkg.X\n',
        "clean_allowed_module_import.go",
        'package p\n\nimport _ "modernc.org/sqlite"\n',
    ),
]

FIXTURES = {
    "d5_bad.txt": "// " + SUPPRESS + "\n",
    "d5_clean.txt": "no marker here\n",
}


def scan(tmp: Path) -> dict:
    out = subprocess.run(
        ["semgrep", "scan", "--config", CONFIG, "--json", "-j", "1", str(tmp)],
        capture_output=True, text=True,
    )
    try:
        return json.loads(out.stdout)
    except json.JSONDecodeError:
        print("semgrep probe scan produced no parseable JSON")
        print(out.stderr)
        sys.exit(1)


def main() -> int:
    tmp = Path(tempfile.mkdtemp(prefix="semgrep-probes-"))
    try:
        (tmp / ".semgrepignore").write_text(".git/\n")
        for _rid, vfile, vcontent, cfile, ccontent in PROBES:
            (tmp / vfile).write_text(vcontent)
            (tmp / cfile).write_text(ccontent)
        d5 = tmp / "d5"
        d5.mkdir()
        (d5 / "bad.txt").write_text(FIXTURES["d5_bad.txt"])
        (d5 / "clean.txt").write_text(FIXTURES["d5_clean.txt"])
        (tmp / "d5clean").mkdir()
        (tmp / "d5clean" / "clean.txt").write_text("nothing\n")

        # a2aclient_dir: the sole surviving path-scoped fixture
        # directory. sdk.go.no-a2aloopback-import still depends on it
        # for its /a2aclient/*_test.go exclude; the five scoped
        # third-party rules that also used to write here are gone.
        a2aclient_dir = tmp / "a2aclient"
        a2aclient_dir.mkdir()

        # no-a2aloopback-import rule pair: proves only the allowed
        # caller paths may import a2aloopback.
        no_a2aloopback_rid = "sdk.go.no-a2aloopback-import"
        (tmp / "viol_a2aloopback_prod_import.go").write_text(
            'package p\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"\n\nvar _ = a2aloopback.Loopback\n'
        )
        (a2aclient_dir / "clean_a2aloopback_caller_import_test.go").write_text(
            'package a2aclient\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"\n\nvar _ = a2aloopback.Loopback\n'
        )

        # Post-write basename-collision check. The block above holds no
        # pre-write registry of its basenames, so this walks what was
        # actually written to tmp and fails loudly on a duplicate
        # before Semgrep runs, not after a confusing count mismatch.
        by_basename: dict[str, list[Path]] = {}
        for f in tmp.rglob("*.go"):
            by_basename.setdefault(f.name, []).append(f)
        collisions = {name: paths for name, paths in by_basename.items() if len(paths) > 1}
        if collisions:
            details = "; ".join(
                f"{name}: {[str(p) for p in paths]}" for name, paths in collisions.items()
            )
            raise AssertionError(f"probe fixture basename collision: {details}")

        data = scan(tmp)
        if data.get("errors"):
            print("semgrep probe scan errors:", data["errors"])
            return 1
        expected = {}
        for rid, vfile, _v, cfile, _c in PROBES:
            expected[vfile] = rid
            expected[cfile] = rid
        expected["viol_a2aloopback_prod_import.go"] = no_a2aloopback_rid
        expected["clean_a2aloopback_caller_import_test.go"] = no_a2aloopback_rid
        hits = {}
        for r in data.get("results", []):
            name = Path(r["path"]).name
            rid = r["check_id"]
            if rid.startswith("semgrep."):
                rid = rid[len("semgrep."):]
            hits.setdefault(name, set()).add(rid)

        problems = []
        for rid, vfile, _v, cfile, _c in PROBES:
            if rid not in hits.get(vfile, set()):
                problems.append(f"{rid}: violation file {vfile} did not fire")
            if rid in hits.get(cfile, set()):
                problems.append(f"{rid}: clean file {cfile} fired")
            for name in (vfile, cfile):
                extra = hits.get(name, set()) - {rid}
                if extra:
                    problems.append(f"{name}: unexpected rules fired: {sorted(extra)}")

        # Explicit no-a2aloopback-import assertions: the rule fires on
        # a production-looking import outside every exclude path, and
        # stays silent on a file matching the /a2aclient/*_test.go
        # exclude.
        no_a2aloopback_viol_hits = hits.get("viol_a2aloopback_prod_import.go", set())
        no_a2aloopback_clean_hits = hits.get("clean_a2aloopback_caller_import_test.go", set())
        if no_a2aloopback_rid not in no_a2aloopback_viol_hits:
            problems.append(f"{no_a2aloopback_rid}: violation file viol_a2aloopback_prod_import.go did not fire")
        if no_a2aloopback_rid in no_a2aloopback_clean_hits:
            problems.append(f"{no_a2aloopback_rid}: clean file clean_a2aloopback_caller_import_test.go fired")

        for name in hits:
            if name not in expected and not name.startswith("d5"):
                problems.append(f"{name}: unlisted probe file fired rules {sorted(hits[name])}")

        grep = subprocess.run(
            ["grep", "-riE", GREP_PATTERN, str(d5)],
            capture_output=True, text=True,
        )
        if grep.returncode != 0:
            problems.append("suppression grep did not find the comment-form marker fixture")
        grep_clean = subprocess.run(
            ["grep", "-riE", GREP_PATTERN, str(tmp / "d5clean")],
            capture_output=True, text=True,
        )
        if grep_clean.returncode == 0:
            problems.append("suppression grep fired on a marker-free directory")

        if problems:
            print("\n".join(problems))
            return 1
        return 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
