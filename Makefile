# Claude Profile Manager — build helpers (macOS/Linux; on Windows use build.ps1)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X claude-profile-manager/internal/cli.Version=$(VERSION)
DIST    := dist

.PHONY: all deps build cli test vet run package clean

all: test build cli

deps:
	go mod tidy

build: deps
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/claude-profile-manager .

cli:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(DIST)/cpm ./cmd/cpm

test:
	go test ./internal/...

vet:
	go vet ./...

run: deps
	go run .

# Native app bundle (.app on macOS, .tar.xz with .desktop file on Linux).
# Requires: go install fyne.io/tools/cmd/fyne@latest
package: deps
	fyne package --release --app-version $(patsubst v%,%,$(VERSION)) --name "Claude Profile Manager"

clean:
	rm -rf $(DIST) *.app *.tar.xz
