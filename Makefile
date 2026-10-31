# Excalibur verification targets. Every target must pass before a wave
# is declared done. Prefer `go test -race -p 2` for package-scoped runs
# while iterating: the machine has 8 CPUs shared with other agents.
.PHONY: verify test lint cross build

verify: lint test cross

test:
	go test ./... -race

lint:
	gofmt -l .
	go vet ./...
	staticcheck ./...
	./scripts/check-no-bare-go.sh

cross:
	GOOS=darwin go build ./...
	GOOS=windows go build ./...

build:
	go build ./...
