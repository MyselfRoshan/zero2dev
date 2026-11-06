.PHONY: web build build-windows test smoke vet fmt clean

GOPKGS := ./cmd/... ./internal/...
VERSION := $(shell cat VERSION | tr -d ' \r\n')
LDFLAGS := -s -w -X main.version=$(VERSION)
UI := internal/webui/dist/index.html

# The Go binary embeds the built UI (internal/webui/dist, not in git), so every Go
# target below builds it first. `make web` forces a rebuild after UI changes.
web:
	cd web && pnpm install --frozen-lockfile && pnpm run build

$(UI):
	$(MAKE) web

build: $(UI)
	go build -ldflags "$(LDFLAGS)" -o dist/zero2dev ./cmd/zero2dev

build-windows: $(UI)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/zero2dev.exe ./cmd/zero2dev

vet: $(UI)
	go vet $(GOPKGS)
	GOOS=windows GOARCH=amd64 go vet $(GOPKGS)

test: vet
	go test $(GOPKGS) -race -count=1

smoke: build
	bash scripts/smoke.sh

fmt:
	gofmt -l -w cmd internal

clean:
	rm -rf internal/webui/dist dist/zero2dev dist/zero2dev.exe
