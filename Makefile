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
	$(SEMGREP_SCAN)
	@if $(MARKER_SCAN); then echo "suppression markers are forbidden"; exit 1; fi

verify: verify-fast
	@set -e; trap 'rm -f cover.out' EXIT; \
	pkgs="$$(go list ./... | grep -v /scripts)"; \
	go test -coverprofile=cover.out $$pkgs; \
	for p in $$pkgs; do \
		grep -q "^$$p/" cover.out || { echo "cover.out lacks a profile for package $$p"; exit 1; }; \
	done; \
	awk -v floor="$(COVERAGE_FLOOR)" '/^mode:/{next} {file=$$1; sub(/:[0-9].*$$/,"",file); sub(/\/[^\/]+$$/,"",file); tot[file]+=$$(NF-1); if ($$NF==0) unc[file]+=$$(NF-1)} END {ts=0; u=0; bad=0; for (p in tot) {ts+=tot[p]; u+=unc[p]; pct=100*(tot[p]-unc[p])/tot[p]; if (pct<floor) {printf "coverage %.1f%% for %s below the %d%% floor\n", pct, p, floor; bad=1}} t=100*(ts-u)/ts; if (t<floor) {printf "coverage %.1f%% below the %d%% floor\n", t, floor; bad=1} exit bad}' cover.out; \
	python3 scripts/check_semgrep_probes.py

bench:
	go test -run=NONE -bench=. -benchmem ./...

api-update:
	@mkdir -p api
	go run scripts/api_surface.go | awk '/^package /{name=$$2} name{print > ("api/" name ".txt")}'

install-hooks:
	git config core.hooksPath .githooks
