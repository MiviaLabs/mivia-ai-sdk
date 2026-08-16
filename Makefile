verify:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	python3 scripts/check_docs.py
	python3 scripts/check_structure.py

bench:
	go test -run=NONE -bench=. -benchmem ./...

install-hooks:
	git config core.hooksPath .githooks
