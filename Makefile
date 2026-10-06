.PHONY: check lint test live run build

check: lint test

lint:
	gofmt -l cmd internal | grep . && exit 1 || true
	go vet ./...
	go vet -tags release ./...
	# Release builds must not carry the demo mode or Studio Weber (as in CI).
	go build -tags release -o bin/andon-release ./cmd/andon && scripts/release-check.sh bin/andon-release

test:
	go test ./...

# Live tests against the instances in .local-test/ (internal/testkit/live).
live:
	ANDON_LIVE=1 go test -count=1 -run Live ./...

VERSION_FLAG = -X andon/internal/services/about.version=$(shell scripts/version.sh)

run:
	DATA_DIR=./data ANDON_DEV=true MASTER_KEY=dev-only-not-secret go run -ldflags="$(VERSION_FLAG)" ./cmd/andon

# Release build: no demo mode, no demo data (scripts/release-check.sh).
build:
	CGO_ENABLED=0 go build -tags release -trimpath -ldflags="-s -w $(VERSION_FLAG)" -o bin/andon ./cmd/andon
	scripts/release-check.sh bin/andon
