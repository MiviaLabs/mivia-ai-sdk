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

        # a2aclient-scoped rule pair: proves the stdlib-only exclude and
        # the new scoped rule fire together, in a path-scoped directory
        # the flat PROBES loop above cannot exercise.
        a2aclient_rid = "sdk.go.a2aclient-scoped-third-party-import"
        a2aclient_dir = tmp / "a2aclient"
        a2aclient_dir.mkdir()
        (a2aclient_dir / "viol_other_import.go").write_text(
            'package a2aclient\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n'
        )
        (a2aclient_dir / "clean_a2a_import.go").write_text(
            'package a2aclient\n\nimport "github.com/a2aproject/a2a-go/a2aclient"\n\nvar _ = a2aclient.Config{}\n'
        )

        # a2aloopback-scoped rule pair: proves the stdlib-only exclude
        # and the new scoped rule fire together, in a path-scoped
        # directory the flat PROBES loop above cannot exercise. The
        # third fixture, clean_a2aloopback_internal_import.go, pins the
        # regression an omitted first pattern-not-regex line would
        # cause: it would fire ERROR on a2aloopback's own required
        # internal import of envelope.
        a2aloopback_rid = "sdk.go.a2aloopback-scoped-third-party-import"
        a2aloopback_dir = tmp / "a2aloopback"
        a2aloopback_dir.mkdir()
        (a2aloopback_dir / "viol_a2aloopback_other_import.go").write_text(
            'package a2aloopback\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n'
        )
        (a2aloopback_dir / "clean_a2a_go_srv_import.go").write_text(
            'package a2aloopback\n\nimport "github.com/a2aproject/a2a-go/a2asrv"\n\nvar _ = a2asrv.AgentExecutor(nil)\n'
        )
        (a2aloopback_dir / "clean_a2aloopback_internal_import.go").write_text(
            'package a2aloopback\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/envelope"\n\nvar _ = envelope.Message{}\n'
        )

        # no-a2aloopback-import rule pair: proves only the allowed
        # caller paths may import a2aloopback.
        no_a2aloopback_rid = "sdk.go.no-a2aloopback-import"
        (tmp / "viol_a2aloopback_prod_import.go").write_text(
            'package p\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"\n\nvar _ = a2aloopback.Loopback\n'
        )
        (a2aclient_dir / "clean_a2aloopback_caller_import_test.go").write_text(
            'package a2aclient\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"\n\nvar _ = a2aloopback.Loopback\n'
        )

        # mcp-scoped rule pair: proves the stdlib-only exclude and the
        # new scoped rule fire together, in a path-scoped directory the
        # flat PROBES loop above cannot exercise.
        mcp_rid = "sdk.go.mcp-scoped-third-party-import"
        mcp_dir = tmp / "mcp"
        mcp_dir.mkdir()
        (mcp_dir / "viol_mcp_other_import.go").write_text(
            'package mcp\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n'
        )
        (mcp_dir / "clean_go_sdk_import.go").write_text(
            'package mcp\n\nimport mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"\n\nvar _ = mcpsdk.Transport(nil)\n'
        )

        # ledger-scoped rule pair: proves the stdlib-only exclude and
        # the new scoped rule fire together, in a path-scoped
        # directory the flat PROBES loop above cannot exercise.
        ledger_rid = "sdk.go.ledger-scoped-third-party-import"
        ledger_dir = tmp / "ledger"
        ledger_dir.mkdir()
        (ledger_dir / "viol_ledger_other_import.go").write_text(
            'package ledger\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n'
        )
        (ledger_dir / "clean_modernc_sqlite_import.go").write_text(
            'package ledger\n\nimport _ "modernc.org/sqlite"\n\nvar _ = 1\n'
        )

        # schema-scoped rule pair: proves the stdlib-only exclude and
        # the new scoped rule fire together, in a path-scoped
        # directory the flat PROBES loop above cannot exercise.
        schema_rid = "sdk.go.schema-scoped-third-party-import"
        schema_dir = tmp / "schema"
        schema_dir.mkdir()
        (schema_dir / "viol_schema_other_import.go").write_text(
            'package schema\n\nimport "github.com/other/pkg"\n\nvar _ = pkg.X\n'
        )
        (schema_dir / "clean_jsonschema_import.go").write_text(
            'package schema\n\nimport jsonschema "github.com/santhosh-tekuri/jsonschema/v6"\n\nvar _ = jsonschema.NewCompiler()\n'
        )

        data = scan(tmp)
        if data.get("errors"):
            print("semgrep probe scan errors:", data["errors"])
            return 1
        expected = {}
        for rid, vfile, _v, cfile, _c in PROBES:
            expected[vfile] = rid
            expected[cfile] = rid
        expected["viol_other_import.go"] = a2aclient_rid
        expected["clean_a2a_import.go"] = a2aclient_rid
        expected["viol_a2aloopback_other_import.go"] = a2aloopback_rid
        expected["clean_a2a_go_srv_import.go"] = a2aloopback_rid
        expected["clean_a2aloopback_internal_import.go"] = a2aloopback_rid
        expected["viol_a2aloopback_prod_import.go"] = no_a2aloopback_rid
        expected["clean_a2aloopback_caller_import_test.go"] = no_a2aloopback_rid
        expected["viol_mcp_other_import.go"] = mcp_rid
        expected["clean_go_sdk_import.go"] = mcp_rid
        expected["viol_ledger_other_import.go"] = ledger_rid
        expected["clean_modernc_sqlite_import.go"] = ledger_rid
        expected["viol_schema_other_import.go"] = schema_rid
        expected["clean_jsonschema_import.go"] = schema_rid
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

        # Explicit a2aclient-scoped assertions, parallel to the PROBES
        # loop above: the scoped rule fires on the violation, stays
        # silent on the clean import, and the scoped exclude keeps
        # stdlib-only-imports out of both.
        viol_hits = hits.get("viol_other_import.go", set())
        clean_hits = hits.get("clean_a2a_import.go", set())
        if a2aclient_rid not in viol_hits:
            problems.append(f"{a2aclient_rid}: violation file viol_other_import.go did not fire")
        if a2aclient_rid in clean_hits:
            problems.append(f"{a2aclient_rid}: clean file clean_a2a_import.go fired")
        if "sdk.go.stdlib-only-imports" in viol_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded a2aclient/ directory")
        if "sdk.go.stdlib-only-imports" in clean_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded a2aclient/ directory")

        # Explicit a2aloopback-scoped assertions, parallel to the
        # a2aclient block above: the scoped rule fires on the
        # violation, stays silent on both clean imports, and the
        # scoped exclude keeps stdlib-only-imports out of all three.
        # clean_a2aloopback_internal_import.go is the regression pin
        # for an omitted first pattern-not-regex line: without it, the
        # rule would fire ERROR on a2aloopback's own required envelope
        # import.
        a2aloopback_other_hits = hits.get("viol_a2aloopback_other_import.go", set())
        a2aloopback_srv_hits = hits.get("clean_a2a_go_srv_import.go", set())
        a2aloopback_internal_hits = hits.get("clean_a2aloopback_internal_import.go", set())
        if a2aloopback_rid not in a2aloopback_other_hits:
            problems.append(f"{a2aloopback_rid}: violation file viol_a2aloopback_other_import.go did not fire")
        if a2aloopback_rid in a2aloopback_srv_hits:
            problems.append(f"{a2aloopback_rid}: clean file clean_a2a_go_srv_import.go fired")
        if a2aloopback_rid in a2aloopback_internal_hits:
            problems.append(f"{a2aloopback_rid}: clean file clean_a2aloopback_internal_import.go fired")
        for name, name_hits in (
            ("viol_a2aloopback_other_import.go", a2aloopback_other_hits),
            ("clean_a2a_go_srv_import.go", a2aloopback_srv_hits),
            ("clean_a2aloopback_internal_import.go", a2aloopback_internal_hits),
        ):
            if "sdk.go.stdlib-only-imports" in name_hits:
                problems.append(f"sdk.go.stdlib-only-imports: fired inside the excluded a2aloopback/ directory ({name})")

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

        # Explicit mcp-scoped assertions, parallel to the a2aclient
        # block above: the scoped rule fires on the violation, stays
        # silent on the clean import, and the scoped exclude keeps
        # stdlib-only-imports out of both. viol_mcp_other_import.go
        # uses a name distinct from viol_other_import.go: the hits and
        # expected dicts above key by basename alone, so reusing the
        # a2aclient probe's basename here would silently overwrite its
        # expected-rule entry instead of adding a second one.
        mcp_viol_hits = hits.get("viol_mcp_other_import.go", set())
        mcp_clean_hits = hits.get("clean_go_sdk_import.go", set())
        if mcp_rid not in mcp_viol_hits:
            problems.append(f"{mcp_rid}: violation file viol_mcp_other_import.go did not fire")
        if mcp_rid in mcp_clean_hits:
            problems.append(f"{mcp_rid}: clean file clean_go_sdk_import.go fired")
        if "sdk.go.stdlib-only-imports" in mcp_viol_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded mcp/ directory")
        if "sdk.go.stdlib-only-imports" in mcp_clean_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded mcp/ directory")

        # Explicit ledger-scoped assertions, parallel to the a2aclient
        # and mcp blocks above: the scoped rule fires on the
        # violation, stays silent on the clean modernc.org/sqlite
        # import, and the scoped exclude keeps stdlib-only-imports out
        # of both.
        ledger_viol_hits = hits.get("viol_ledger_other_import.go", set())
        ledger_clean_hits = hits.get("clean_modernc_sqlite_import.go", set())
        if ledger_rid not in ledger_viol_hits:
            problems.append(f"{ledger_rid}: violation file viol_ledger_other_import.go did not fire")
        if ledger_rid in ledger_clean_hits:
            problems.append(f"{ledger_rid}: clean file clean_modernc_sqlite_import.go fired")
        if "sdk.go.stdlib-only-imports" in ledger_viol_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded ledger/ directory")
        if "sdk.go.stdlib-only-imports" in ledger_clean_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded ledger/ directory")

        # Explicit schema-scoped assertions, parallel to the a2aclient,
        # mcp, and ledger blocks above: the scoped rule fires on the
        # violation, stays silent on the clean jsonschema/v6 import,
        # and the scoped exclude keeps stdlib-only-imports out of both.
        schema_viol_hits = hits.get("viol_schema_other_import.go", set())
        schema_clean_hits = hits.get("clean_jsonschema_import.go", set())
        if schema_rid not in schema_viol_hits:
            problems.append(f"{schema_rid}: violation file viol_schema_other_import.go did not fire")
        if schema_rid in schema_clean_hits:
            problems.append(f"{schema_rid}: clean file clean_jsonschema_import.go fired")
        if "sdk.go.stdlib-only-imports" in schema_viol_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded schema/ directory")
        if "sdk.go.stdlib-only-imports" in schema_clean_hits:
            problems.append("sdk.go.stdlib-only-imports: fired inside the excluded schema/ directory")

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
