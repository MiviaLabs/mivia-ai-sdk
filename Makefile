COVERAGE_FLOOR := 85
SEMGREP_SCAN := semgrep scan --config semgrep/ --error --metrics=off --quiet -j 1 .

# verify-fast is the local tier: the pre-commit hook runs it on the staged
# snapshot. verify is the full tier: it adds the coverage floor and the
# semgrep probe suite. Never weaken a gate to make a change pass.
verify-fast:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go vet ./docs/examples/_agentloop/
	go run ./docs/examples/_agentloop/ | grep -q '^final: HELLO$$'
	go vet ./docs/examples/_agentloop_minimal/
	go run ./docs/examples/_agentloop_minimal/ | grep -q '^final: HELLO$$'
	go vet ./docs/examples/_agentloop_adoption/
	go run ./docs/examples/_agentloop_adoption/ | grep -q '^final: HELLO$$'
	go test ./...
	python3 scripts/check_docs.py
	python3 scripts/check_structure.py
	python3 scripts/check_deps.py
	python3 scripts/check_plan.py
	python3 scripts/check_orphan_packages.py
	python3 scripts/check_symbol_wiring.py
	python3 scripts/check_prose.py
	python3 scripts/check_api.py
	python3 scripts/check_thirdparty.py
	python3 scripts/check_semgrepignore.py
		python3 scripts/check_labels.py
		python3 scripts/check_names.py
		python3 scripts/check_examples_sync.py
	python3 scripts/check_test_tampering.py
	python3 scripts/agent_hook_guard.py --probe
	python3 scripts/check_timeout_saturation.py --probe
	python3 scripts/check_docs.py --probe
	python3 scripts/check_timeout_saturation.py
	$(SEMGREP_SCAN)

# The race step gates every "run under go test -race" comment in the
# tree. It runs over the default build, after verify-fast and before
# the coverage block. verify-fast stays free of it, so the pre-commit
# hook keeps its runtime.
verify: verify-fast verify-ledger-sqlite
	go test -race ./...
	@set -e; trap 'rm -f cover.out cover_*.out' EXIT; \
	src_pkgs="$$(go list ./... | grep -v /scripts | grep -v '_test$$')"; \
	mod="$$(head -1 go.mod | awk '{print $$2}')"; \
	rm -f cover.out; \
	for p in $$src_pkgs; do \
		pf="$$(echo $$p | tr '/' '_')"; \
		rel="$${p#$$mod/}"; \
		last="$$(basename $$rel)"; \
		tp="$$rel/$${last}_test"; \
		if [ -d "$$tp" ]; then \
			go test -coverprofile=cover_$${pf}.out -coverpkg=$$p ./$$tp; \
		else \
			go test -coverprofile=cover_$${pf}.out $$p; \
		fi; \
	done; \
	echo "mode: set" > cover.out; \
	for f in cover_*.out; do grep -v "^mode:" "$$f" >> cover.out 2>/dev/null || true; done; \
	for p in $$src_pkgs; do \
		grep -q "^$$p/" cover.out || { echo "cover.out lacks a profile for package $$p"; exit 1; }; \
	done; \
	awk -v floor="$(COVERAGE_FLOOR)" '/^mode:/{next} {file=$$1; sub(/:[0-9].*$$/,"",file); sub(/\/[^\/]+$$/,"",file); tot[file]+=$$(NF-1); if ($$NF==0) unc[file]+=$$(NF-1)} END {ts=0; u=0; bad=0; for (p in tot) {ts+=tot[p]; u+=unc[p]; pct=100*(tot[p]-unc[p])/tot[p]; if (pct<floor) {printf "coverage %.1f%% for %s below the %d%% floor\n", pct, p, floor; bad=1}} t=100*(ts-u)/ts; if (t<floor) {printf "coverage %.1f%% below the %d%% floor\n", t, floor; bad=1} exit bad}' cover.out; \
	# The x/ sub-module is a nested module, invisible to the root
	# go list, so the root test block never sees it. Verify it here
	# so CI keeps testing the quarantine.
	go test -C x ./...
	python3 scripts/check_semgrep_probes.py
	python3 scripts/check_mutation.py --probe
	python3 scripts/check_orphan_packages.py --probe
	python3 scripts/check_symbol_wiring.py --probe
	python3 scripts/check_deps.py --probe
	python3 scripts/check_plan.py --probe
	python3 scripts/check_api.py --probe
	python3 scripts/check_thirdparty.py --probe
	python3 scripts/check_test_tampering.py --probe

bench:
	go test -run=NONE -bench=. -benchmem ./...

# mutation runs the full per-package mutation sweep on demand. It never
# runs inside verify or verify-fast; a full sweep costs minutes, not
# seconds. Pass PKG to name one package, e.g. `make mutation PKG=ledger`.
mutation:
	python3 scripts/check_mutation.py --pkg $(PKG)

# mutation-gate runs a full sweep against every package that holds a
# scripts/mutation_denylist/<pkg>.json floor, one package at a time,
# and fails if any drops below its own stored floor. It never runs
# inside verify or verify-fast: see docs/plans/agents/phase74_mutation_coverage_rollout.md,
# "Verify wiring". check_mutation.py's finally block restores a
# mutated file on a normal interrupt, but not on an external hard
# kill, so this target ends with a git diff check: a leftover mutant
# left on disk by a killed sweep fails the target loudly instead of
# merging silently.
mutation-gate:
	@set -e; for f in scripts/mutation_denylist/*.json; do \
		pkg="$$(basename $$f .json)"; \
		echo "mutation-gate: $$pkg"; \
		python3 scripts/check_mutation.py --pkg $$pkg || exit 1; \
	done
	git diff --exit-code

# verify-ledger-sqlite is the tag-gated verification command for
# SQLiteStore (docs/plans/agents/phase42_ledger_durable_store.md).
# sqlite_store*.go never compiles into the default build, so verify
# depends on this target for the tagged tier. It holds the tag-gated
# ledger package to the same 85% coverage floor verify's default
# build block enforces, and it also runs e2e's tagged ceremony
# scenario over a SQLite file, so the durable store is exercised at
# the composition layer, not only alone.
verify-ledger-sqlite:
	@trap 'rm -f cover_ledger_sqlite.out' EXIT; \
	go test -tags ledger_sqlite -race -coverprofile=cover_ledger_sqlite.out -coverpkg=./ledger ./ledger/... ./internal/e2e/...; \
	go tool cover -func=cover_ledger_sqlite.out | awk -v floor="$(COVERAGE_FLOOR)" '/^total:/{pct=$$3; sub(/%/,"",pct); if (pct+0<floor) {printf "ledger (tag ledger_sqlite) coverage %.1f%% below the %d%% floor\n", pct, floor; exit 1}}'

# api-update rewrites every lock under api/ from the tool output. The
# lock path mirrors the package path, so a nested package writes a
# nested lock. A shell loop replaces the former awk one-liner: awk
# cannot create the parent directory and aborts the whole stream on the
# first nested package. The loop truncates a lock on its `package `
# header and appends every following line, so a flat lock stays
# byte-identical. A stale lock stays on disk; check_api.py reports it.
api-update:
	@set -e; \
	tmp="$$(mktemp)"; trap 'rm -f "$$tmp"' EXIT; \
	go run scripts/api_surface.go > "$$tmp"; \
	mkdir -p api; \
	out=""; \
	while IFS= read -r line; do \
		case "$$line" in \
		"package "*) \
			out="api/$${line#package }.txt"; \
			mkdir -p "$$(dirname "$$out")"; \
			: > "$$out"; \
			;; \
		esac; \
		if [ -n "$$out" ]; then printf '%s\n' "$$line" >> "$$out"; fi; \
	done < "$$tmp"

# thirdparty-update rewrites policy/thirdparty_closure.txt from the
# go.sum module set: one module path per line, sorted, newline
# terminated. It never runs inside verify or verify-fast; commit its
# diff deliberately, alongside the go.sum change that caused it.
thirdparty-update:
	@awk '{print $$1}' go.sum | sort -u > policy/thirdparty_closure.txt

install-hooks:
	git config core.hooksPath .githooks

# lint-unparam is optional and never runs inside verify-fast or
# verify: it needs golangci-lint installed locally, and this repo
# must stay verifiable by a stranger with only Go and Python. It
# checks one linter, unparam, against .golangci.yml. Run it by hand.
lint-unparam:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "lint-unparam: golangci-lint not installed, skipping optional check"; exit 0; }
	golangci-lint run --config .golangci.yml

# advisory runs checks that report a real signal but stay out of
# verify and verify-fast on purpose: each one trades recall for
# precision, or names a legitimate pattern often enough that a hard
# fail would be noise. Run it by hand; it never blocks a commit or CI.
advisory:
	python3 scripts/check_interface_implementers.py
