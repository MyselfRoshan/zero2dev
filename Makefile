.PHONY: web build build-windows test smoke vet fmt

GOPKGS := ./cmd/... ./internal/...
VERSION := $(shell cat VERSION | tr -d ' \r\n')
LDFLAGS := -s -w -X main.version=$(VERSION)

# Build the Svelte UI into internal/webui/dist (uses vp, falls back to npm).
web:
	cd web && (command -v vp >/dev/null 2>&1 && vp build || npm run build)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/zero2dev ./cmd/zero2dev

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/zero2dev.exe ./cmd/zero2dev

test:
	go vet $(GOPKGS)
	GOOS=windows GOARCH=amd64 go vet $(GOPKGS)
	go test $(GOPKGS) -race -count=1

smoke: build
	bash scripts/smoke.sh

fmt:
	gofmt -l -w cmd internal
