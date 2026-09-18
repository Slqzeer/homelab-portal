.PHONY: dev web-build test lint build

# Start the fixture-backed Astro preview with live reload.
dev:
	npm --prefix web run dev

# Install exactly the locked frontend dependencies and stage the static build.
web-build:
	npm --prefix web ci
	npm --prefix web run build

# Run all behavior tests against freshly built embedded assets.
test: web-build
	go test ./...

# Run frontend checks and Go static analysis.
lint: web-build
	npm --prefix web run lint
	go vet ./...

# Produce the single local portal executable with embedded assets.
build: web-build
	go build -trimpath -o portal ./cmd/portal
