.PHONY: check fmt-check vet test coverage docs api demo build
.NOTPARALLEL:

check: fmt-check vet test coverage docs build

fmt-check:
	@test -z "$$(gofmt -l cordis examples scripts)" || { gofmt -l cordis examples scripts; exit 1; }

vet:
	go vet ./...

test:
	go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...

coverage:
	go run ./scripts/doccheck -coverage coverage.out

docs:
	go run ./scripts/doccheck

api:
	go run ./scripts/doccheck -write-api

demo:
	go run ./examples/cordis

build:
	go build ./...
