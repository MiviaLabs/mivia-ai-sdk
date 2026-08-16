COVERAGE_FLOOR := 85

verify:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	python3 scripts/check_docs.py
	python3 scripts/check_structure.py
	python3 scripts/check_deps.py
	python3 scripts/check_plan.py
	python3 scripts/check_prose.py
	python3 scripts/check_api.py
	semgrep scan --config semgrep/ --error --metrics=off --quiet .
	@pkgs=$$(go list ./... | grep -v /scripts); \
	go test -coverprofile=cover.out $$pkgs >/dev/null 2>&1; \
	total=$$(go tool cover -func=cover.out | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	rm -f cover.out; \
	awk -v t="$$total" -v f="$(COVERAGE_FLOOR)" 'BEGIN { if (t+0 < f) { printf "coverage %s%% is below the %s%% floor\n", t, f; exit 1 } }'

bench:
	go test -run=NONE -bench=. -benchmem ./...

api-update:
	@mkdir -p api
	go run scripts/api_surface.go | awk '/^package /{name=$$2} name{print > ("api/" name ".txt")}'

install-hooks:
	git config core.hooksPath .githooks
