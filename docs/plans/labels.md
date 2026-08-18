# Plan: labels

## Goal

Stop agent-internal audit-finding labels from appearing in comments, docs, and
plans. Add a tree-wide text gate that fails any file holding a label. Keep the
rule narrow so it never flags ordinary prose.

## Scope

Inside: the new gate script, its Makefile wiring, the rewrite of four files,
two new AGENTS.md items, and this plan. Outside: Go code, `api/` locks,
`policy/layers.json`, `semgrep/` rules, README.md, and
`docs/architecture.md` stay untouched; they are label-free today.

### The rule

- A label is a whole word: a letter A through G followed by a digit.
  The pattern is `\b[A-G][0-9]\b`.
- The scan covers the whole tree except `.git/` and `semgrep/`; this mirrors
  the Makefile marker-scan exclusions.
- The scan walks the physical tree with pathlib and needs no Git repository.
  It runs on the staged snapshot that the pre-commit hook exports, which has
  no `.git` directory.
- The scan reads every file as bytes and never decodes text, so binary files
  cannot crash it. An unreadable file is reported and skipped.
- A hit reports the file path and line number; the script exits one.
  A clean tree exits zero.
- The script follows the existing scripts style: standard library only, a
  header docstring, a `main()` that returns the exit code, and
  `sys.exit(main())` at the bottom.
- Lowercase variants and separated variants (a hyphen, a space, a dot) are
  not labels. The directive's examples are uppercase only. The probe script
  holds the lowercase d5 fixture identifiers; they must not trip the gate on
  the file meant to pass. Review is the backstop for evasive spellings.
- The script scans itself, the Makefile, AGENTS.md, and every plan. No
  committed file may hold a literal example label. Descriptions use words,
  such as "a letter A through G followed by a digit", never an example token.

### Rewrite of the four files

- `docs/plans/gates.md`: delete the label prefix (a letter, a digit, and a
  colon) from every bullet in sections A through G and keep the bullet body;
  the body already describes the fix. Three bullets carry a descriptor
  between the label and the colon: "import rule", "drift rule", and "probes".
  For those, delete the label token and the following space, and keep the
  descriptor. Keep the section headings; a bare letter is not a label. Keep
  "Commit 1" and "Commit 2"; they are not labels.
  Reword inline cross-references with phrases: the best-effort framing, the
  api_surface fixes, the semgrep rule fixes, the suppression probe, the
  suppression scan, the suppression grep, the go.mod gate, the hook-guard
  attack strings, the code fixes, the per-package floor, the review-only
  limit. Keep every statement; doc truth matters in an executed plan.
- `scripts/check_semgrep_probes.py`: reword the four references to the
  suppression grep. They are the docstring, the comment, and the two
  error-message strings. The lowercase fixture and directory names stay;
  lowercase variants are out of scope by design.
- `AGENTS.md`: reword the two references to the suppression-marker scan. Add
  `check_labels` to the Layout scripts bullet. Add one Rules item and one
  Enforcement-ladder line that describe the gate without literal examples.
- `docs/plans/room.md`: reword the parenthetical cross-reference in the API
  section to point at the api_surface fixes.

### Rejected alternatives

- Project-name blacklist: rejected. It needs a curated list, and it would
  flag legitimate technical names, such as the protocol name "A2A" in
  `docs/architecture.md` and `docs/plans/a2a.md`. The label ban, the
  writing standard, and adversarial review cover the intent.
- Lowercase and spaced variants: rejected. Lowercase stays out of scope
  because the directive's examples are uppercase and the d5 fixture
  identifiers must keep passing. A separated pair reads as ordinary prose.
  Review is the backstop for evasive spellings.
- Commit-message scanning: out of scope. The directive covers comments, docs,
  and plans. The pre-commit hook gates files on the staged snapshot; no
  commit-msg hook exists, and this change does not invent one.
- Semgrep rule form: rejected. A text scan cannot be silenced by a marker; a
  semgrep rule could be. The existing marker scan and drift rule already
  police markers.

## API

- No Go package changes; `policy/layers.json` is unchanged.
- No exported symbol changes; `api/` locks stay untouched.
- The change's contract is the gate script: exit zero on a clean tree, exit
  one with a path and line report on any hit.
- The Makefile target `verify-fast` runs the script after
  `scripts/check_semgrepignore.py` and before the semgrep scan. The pre-commit
  hook then gates the staged snapshot with it; no hook change is needed.

## Tests

- Probe the gate both ways. A temporary tree with one planted letter-plus-
  digit token must fail with a path and line report. A clean temporary tree
  must pass.
- Run the script on the staged-snapshot shape: export the tree with git
  archive and extract it to a temp dir with no `.git`. Run the script there;
  it must pass.
- The rewrite removes every occurrence in the four files; the full-tree scan
  in verify proves it.
- The script passes on its own tree, including its docstring and this plan.

## Verification

- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_deps.py` passes.
- `python3 scripts/check_prose.py` passes on every plan, including the
  rewritten `docs/plans/gates.md` and this plan.
- `make verify` passes on the final tree, including the new gate.
