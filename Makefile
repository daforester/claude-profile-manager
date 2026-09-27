# Claude Profile Manager — build helpers (macOS/Linux; on Windows use build.ps1)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X claude-profile-manager/internal/cli.Version=$(VERSION)
DIST    := dist

# `make install` target (Linux): per-user by default; PREFIX=/usr/local with
# sudo for everyone. The icon name must match the .desktop file's Icon=, and
# StartupWMClass the main window's title, for launchers and docks to show it.
PREFIX  ?= $(HOME)/.local
APPID   := io.github.claudeprofilemanager
ICONDIR := $(PREFIX)/share/icons/hicolor/512x512/apps
APPSDIR := $(PREFIX)/share/applications

.PHONY: all deps build cli test vet run package install uninstall clean

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
# On macOS the .app also declares the claude:// scheme for sign-in routing.
# Requires: go install fyne.io/tools/cmd/fyne@latest
package: deps
	fyne package --release --app-version $(patsubst v%,%,$(VERSION)) --name "Claude Profile Manager"
	if [ "$$(uname)" = Darwin ]; then scripts/macos-url-scheme.sh "Claude Profile Manager.app"; fi

# Installs the GUI, the cpm CLI, the icon and a launcher. Linux executables
# can't carry an icon; the desktop takes it from the launcher instead.
install: build cli
	install -Dm755 $(DIST)/claude-profile-manager $(PREFIX)/bin/claude-profile-manager
	install -Dm755 $(DIST)/cpm $(PREFIX)/bin/cpm
	install -Dm644 assets/icon.png $(ICONDIR)/$(APPID).png
	mkdir -p $(APPSDIR)
	printf '%s\n' '[Desktop Entry]' 'Type=Application' 'Name=Claude Profile Manager' \
		'Comment=Keep several Claude accounts signed in at once' \
		'Exec=$(PREFIX)/bin/claude-profile-manager' 'Icon=$(APPID)' \
		'Categories=Utility;' 'StartupWMClass=Claude Profile Manager' \
		> $(APPSDIR)/$(APPID).desktop
	@# Refresh an existing icon cache; creating one would hide icons that
	@# other apps add later without updating it.
	@if [ -f $(PREFIX)/share/icons/hicolor/icon-theme.cache ] && command -v gtk-update-icon-cache >/dev/null; then \
		gtk-update-icon-cache -q -t $(PREFIX)/share/icons/hicolor; fi

uninstall:
	rm -f $(PREFIX)/bin/claude-profile-manager $(PREFIX)/bin/cpm \
		$(ICONDIR)/$(APPID).png $(APPSDIR)/$(APPID).desktop

clean:
	rm -rf $(DIST) *.app *.tar.xz
