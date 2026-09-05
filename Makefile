# Convenience tasks for the roamming desktop app.

LOGO_SRC := docs/design/favicon.png
APP_ICON := internal/ui/appicon.png

.PHONY: update-logo icons dist-linux build test cgocheck

# Syntax-check the darwin-only cgo + Objective-C bridge on any host.
# GOOS=darwin go vet cannot run without a macOS toolchain, so translate
# the Go/cgo sources with the platform-independent `go tool cgo` and
# parse the results (plus the .m file) with the host clang against
# stub AppKit headers. Catches prose-in-preamble, missing declarations
# and .m syntax errors before CI does. Needs clang.
cgocheck:
	go run ./tools/cgocheck

# Re-embed the canonical logo as the app/tray icon. Rebuild afterwards:
# the PNG is compiled into the binary via go:embed.
update-logo:
	test -f $(LOGO_SRC)
	cp $(LOGO_SRC) $(APP_ICON)
	@echo "app icon updated ($(APP_ICON)); run 'make build' to embed it"

# Regenerate the Linux desktop icon set (hicolor + pixmaps) from the
# canonical logo. Output is committed; run this after changing the logo.
icons:
	go run ./tools/genicons

# Local dry run of the Linux pipeline: archives + deb/rpm/arch packages
# into dist/. Needs GoReleaser and the Fyne Linux dev packages.
dist-linux:
	goreleaser release --snapshot --clean -f .goreleaser/linux.yaml

build:
	go build -o roamming .

test:
	go test ./...
