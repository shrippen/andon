.PHONY: check lint test run build

check: lint test

lint:
	gofmt -l cmd internal | grep . && exit 1 || true
	go vet ./...
	go vet -tags release ./...

test:
	go test ./...

run:
	DATA_DIR=./data ANDON_DEV=true MASTER_KEY=dev-only-not-secret go run ./cmd/andon

# Release build: no demo mode, no demo data (scripts/release-check.sh).
build:
	CGO_ENABLED=0 go build -tags release -trimpath -ldflags="-s -w" -o bin/andon ./cmd/andon
	scripts/release-check.sh bin/andon
