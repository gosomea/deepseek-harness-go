.PHONY: check fmt-check vet test coverage docs doc-examples api demo parity build
.NOTPARALLEL:

check: fmt-check vet test coverage docs doc-examples build

fmt-check:
	go run ./scripts/doccheck -fmt

vet:
	go vet ./...

test:
	go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...

coverage:
	go run ./scripts/doccheck -coverage coverage.out

docs:
	go run ./scripts/doccheck

doc-examples:
	go run ./scripts/doccheck -examples

api:
	go run ./scripts/doccheck -write-api

demo:
	go run ./examples/cordis

# parity runs the two differential families against their recorded reference
# traces. It is not part of `check`: the live half needs the fixed reference
# build (DSH_PACKAGES / CORDIS_REFERENCE_LIB) and is skipped without it, so
# gating check on it would report an environment as a result.
parity:
	go test -count=1 -run 'Parity|Reference' ./internal/testkit

build:
	go build ./...
