# EML Xtract

A small native macOS utility that pulls attachments out of `.eml` and Outlook `.msg`
files. Drop files (or whole folders) onto the window and each email's attachments land
in their own subfolder of the output directory.

Built with [Wails](https://wails.io) (Go backend, web-rendered UI). `.eml` decoding is
delegated to [ripMIME](https://pldaniels.com/ripmime/); `.msg` files are parsed
natively in Go (OLE compound file → `__attach_version1.0_#…` storages).

## Prerequisites

```bash
brew install ripmime
```

The app checks for `ripmime` at startup (PATH, then `/opt/homebrew/bin` and
`/usr/local/bin`) and shows a red badge if it is missing. `.msg` extraction works
without it.

For development you also need Go 1.25+, Node, and the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

## Build & run

```bash
make build      # → build/bin/eml_xtract.app
make open       # build, then launch
make dev        # live-reload dev mode
make test       # Go unit tests (uses testdata/*.msg and a generated .eml)
```

## Behaviour

- **One folder per email.** `<output>/<email basename>/…`; a second email with the
  same name gets `-2`, `-3`, …, so attachments from different messages never overwrite
  each other.
- **Attachments only by default.** ripMIME is run with `--no-nameless`, so message
  body parts (`textfile0`…) are not written. Tick *Also save unnamed parts* to keep
  body text and unnamed inline images.
- **Safe subprocess call.** ripMIME is invoked with an argv slice
  (`-i <file> -d <dir> --overwrite --stderr --verbose-defects`), never a shell string,
  and has a two-minute timeout per file.
- **Failure detection.** ripMIME 1.4 exits 0 and prints nothing for garbage,
  truncated, or missing input, so the app adds its own checks: an RFC 822 header
  sniff (hard error), a missing closing MIME boundary (warning), and a
  declared-vs-extracted attachment count (warning). ripMIME's exit code and stderr are
  still surfaced when they say anything.
- **`.msg` files.** Binary attachments (`PR_ATTACH_DATA_BIN`) are written using the long
  filename, short filename, or display name, in that order. Embedded Outlook messages
  and OLE objects are reported as skipped rather than silently dropped.
- Empty output folders are removed so a corpus of attachment-less emails does not
  litter the output directory.
- The output folder defaults to `~/Downloads/EML Xtract` and is remembered between runs.

## Layout

| File | Purpose |
|------|---------|
| `main.go` | Wails app options (native file drop enabled) |
| `app.go` | Bound methods: status, dialogs, batch runner, open/reveal |
| `extract.go` | Per-file orchestration, ripMIME runner, folder naming |
| `emlcheck.go` | Structural sanity checks for `.eml` input |
| `msg.go` | Outlook `.msg` attachment extraction |
| `frontend/` | Vanilla JS + Vite UI |

## Distribution (signed + notarized .pkg)

`Makefile-mac-arm` and `Makefile-mac-intel` build a Developer ID-signed, notarized
installer that puts `EML Xtract.app` in `/Applications`:

```bash
make pkg-arm     # → prebuilt/emlxtract-mac-arm-installer.pkg   (Apple Silicon)
make pkg-intel   # → prebuilt/emlxtract-mac-intel-installer.pkg (x86_64, cross-compiled)
make pkg-all
```

Each runs: `wails build -platform darwin/<arch>` → stage into `dist/mac-<arch>/pkg-root`
→ `codesign --options runtime --timestamp` → `pkgbuild --sign` (with
`BundleIsRelocatable` forced off so the installer never "updates" a stray copy in
`build/bin`) → `notarytool submit --wait` → `stapler staple` → `spctl --assess`.

Requirements: *Developer ID Application* and *Developer ID Installer* certificates in
the login keychain, and a notarytool keychain profile named `mxmcp-profile`
(`xcrun notarytool store-credentials mxmcp-profile …`). Bump `VERSION` in both
Makefiles together with `info.productVersion` in `wails.json`.

To test the packaging without signing or notarizing:

```bash
make -f Makefile-mac-arm unsigned
```

The installers do not bundle ripMIME; users need `brew install ripmime` for `.eml` files.
