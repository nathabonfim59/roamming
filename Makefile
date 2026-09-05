# Convenience tasks for the roamming desktop app.

LOGO_SRC := docs/design/favicon.png
APP_ICON := internal/ui/appicon.png

.PHONY: update-logo build test

# Re-embed the canonical logo as the app/tray icon. Rebuild afterwards:
# the PNG is compiled into the binary via go:embed.
update-logo:
	test -f $(LOGO_SRC)
	cp $(LOGO_SRC) $(APP_ICON)
	@echo "app icon updated ($(APP_ICON)); run 'make build' to embed it"

build:
	go build -o roamming .

test:
	go test ./...
