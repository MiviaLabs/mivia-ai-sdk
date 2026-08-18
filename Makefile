COVERAGE_FLOOR := 85
SEMGREP_SCAN := semgrep scan --config semgrep/ --error --metrics=off --quiet -j 1 .
MARKER_SCAN := grep -riE '(//|\#)\s*nosem[g]rep' . --exclude-dir=.git --exclude-dir=semgrep

# verify-fast is the local tier: the pre-commit hook runs it on the staged
# snapshot. verify is the full tier: it adds the coverage floor and the
# semgrep probe suite. Never weaken a gate to make a change pass.
verify-fast:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	python3 scripts/check_docs.py
	python3 scripts/check_structure.py
	python3 scripts/check_deps.py
	python3 scripts/check_plan.py
	python3 scripts/check_prose.py
	python3 scripts/check_api.py
	python3 scripts/check_gomod.py
	python3 scripts/check_semgrepignore.py
		python3 scripts/check_labels.py
		python3 scripts/check_names.py
		python3 scripts/check_examples_sync.py
	$(SEMGREP_SCAN)
	@if $(MARKER_SCAN); then echo "suppression markers are forbidden"; exit 1; fi

verify: verify-fast
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
	python3 scripts/check_semgrep_probes.py

bench:
	go test -run=NONE -bench=. -benchmem ./...

# verify-ledger-sqlite is the tag-gated verification command for
# SQLiteStore (docs/plans/agents/phase42_ledger_durable_store.md). It
# is not part of the default verify target: sqlite_store*.go never
# compiles into the default build. It holds the tag-gated ledger
# package to the same 85% coverage floor verify's default-build block
# enforces.
verify-ledger-sqlite:
	@trap 'rm -f cover_ledger_sqlite.out' EXIT; \
	go test -tags ledger_sqlite -race -coverprofile=cover_ledger_sqlite.out -coverpkg=./ledger ./ledger/...; \
	go tool cover -func=cover_ledger_sqlite.out | awk -v floor="$(COVERAGE_FLOOR)" '/^total:/{pct=$$3; sub(/%/,"",pct); if (pct+0<floor) {printf "ledger (tag ledger_sqlite) coverage %.1f%% below the %d%% floor\n", pct, floor; exit 1}}'

api-update:
	@mkdir -p api
	go run scripts/api_surface.go | awk '/^package /{name=$$2} name{print > ("api/" name ".txt")}'

install-hooks:
	git config core.hooksPath .githooks
