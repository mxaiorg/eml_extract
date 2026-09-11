.PHONY: dev build build-universal test clean open pkg-arm pkg-intel pkg-all

# Live-reload development (Go + Vite). Needs: wails, node, ripmime.
dev:
	wails dev

# Production .app bundle → build/bin/eml_xtract.app
build:
	wails build -clean

# Universal (arm64 + amd64) build for sharing with other Macs.
build-universal:
	wails build -clean -platform darwin/universal

test:
	go test ./...

open: build
	open "build/bin/eml_xtract.app"

clean:
	rm -rf build/bin frontend/dist/* frontend/node_modules
	touch frontend/dist/gitkeep

# Signed + notarized .pkg installers (see Makefile-mac-arm / Makefile-mac-intel).
pkg-arm:
	$(MAKE) -f Makefile-mac-arm

pkg-intel:
	$(MAKE) -f Makefile-mac-intel

pkg-all: pkg-arm pkg-intel
