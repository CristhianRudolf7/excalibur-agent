# Excalibur verification targets. Every target must pass before a wave
# is declared done. Prefer `go test -race -p 2` for package-scoped runs
# while iterating: the machine has 8 CPUs shared with other agents.
.PHONY: verify test lint cross build web

verify: lint test cross

test:
	go test ./... -race

lint:
	gofmt -l .
	go vet ./...
	staticcheck ./...
	./scripts/check-no-bare-go.sh

cross:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...

build:
	go build ./...

# The placeholder keeps ordinary Go builds independent of Node.
web:
	pnpm --filter @excalibur/web build
	node scripts/release/embed-web.mjs
