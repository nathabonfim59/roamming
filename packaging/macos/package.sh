#!/usr/bin/env bash
# Builds the macOS .pkg installers from the darwin binaries goreleaser
# produced in dist/ (run on a macOS runner; sips/iconutil/pkgbuild/
# codesign/lipo are native tools).
#
# Usage: VERSION=v1.2.3 packaging/macos/package.sh   (v prefix optional)
#
# Produces one .pkg per arch plus a universal (Intel + Apple Silicon)
# build. Every package installs /Applications/roamming.app (agent app:
# no Dock icon, tray-first) and registers a LaunchAgent so it starts at
# login - the macOS equivalent of the Linux systemd user unit and the
# Windows HKCU Run autostart entry.
set -euo pipefail

VERSION="${VERSION:?VERSION must be set (e.g. v1.2.3 or 1.2.3)}"
VERSION="${VERSION#v}"
if ! [[ "$VERSION" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
  echo "warning: VERSION='$VERSION' is not numeric (dry run?); using 0.0.0"
  VERSION="0.0.0"
fi
IDENT="com.nathabonfim59.roamming"
ICON_SRC="docs/design/favicon.png"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

command -v pkgbuild >/dev/null || { echo "pkgbuild not found (run on macOS)" >&2; exit 1; }

WORK="dist/pkg-macos"
rm -rf "$WORK"
mkdir -p "$WORK"

# --- app icon: favicon.png -> roamming.icns ---------------------------
ICONSET="$WORK/roamming.iconset"
mkdir -p "$ICONSET"
sips -z 16 16     "$ICON_SRC" --out "$ICONSET/icon_16x16.png"      >/dev/null
sips -z 32 32     "$ICON_SRC" --out "$ICONSET/icon_16x16@2x.png"   >/dev/null
sips -z 32 32     "$ICON_SRC" --out "$ICONSET/icon_32x32.png"      >/dev/null
sips -z 64 64     "$ICON_SRC" --out "$ICONSET/icon_32x32@2x.png"   >/dev/null
sips -z 128 128   "$ICON_SRC" --out "$ICONSET/icon_128x128.png"    >/dev/null
sips -z 256 256   "$ICON_SRC" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
sips -z 256 256   "$ICON_SRC" --out "$ICONSET/icon_256x256.png"    >/dev/null
sips -z 512 512   "$ICON_SRC" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
sips -z 512 512   "$ICON_SRC" --out "$ICONSET/icon_512x512.png"    >/dev/null
iconutil -c icns "$ICONSET" -o "$WORK/roamming.icns"

# --- collect the goreleaser-built binaries ----------------------------
# (plain vars, not associative arrays: the macOS runner's /bin/bash is
# the GPLv2 bash 3.2, which has no `declare -A`)
BIN_AMD64=""
BIN_ARM64=""
while IFS= read -r bin; do
  case "$bin" in
    *darwin_arm64*) BIN_ARM64="$bin" ;;
    *darwin_amd64*) BIN_AMD64="$bin" ;;
  esac
done < <(find dist -type f -name roamming)
[ -n "$BIN_AMD64" ] || [ -n "$BIN_ARM64" ] || {
  echo "no darwin binaries found in dist/" >&2
  exit 1
}

# --- bundle + package --------------------------------------------------
build_pkg() { # <suffix> <binary>
  local suffix="$1" bin="$2"
  local approot="$WORK/pkg-$suffix/roamming.app"
  mkdir -p "$approot/Contents/MacOS" "$approot/Contents/Resources"
  cp "$bin" "$approot/Contents/MacOS/roamming"
  cp "$WORK/roamming.icns" "$approot/Contents/Resources/roamming.icns"

  cat > "$approot/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>roamming</string>
  <key>CFBundleIconFile</key>
  <string>roamming</string>
  <key>CFBundleIdentifier</key>
  <string>$IDENT</string>
  <key>CFBundleName</key>
  <string>roamming</string>
  <key>CFBundleDisplayName</key>
  <string>roamming</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$VERSION</string>
  <key>CFBundleVersion</key>
  <string>$VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <!-- tray-first agent app: no Dock icon (windows still show) -->
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST

  # ad-hoc signature: required for arm64, keeps Gatekeeper quiet for
  # direct downloads (pkg is not developer-ID/notarized)
  codesign --force --sign - "$approot"

  pkgbuild \
    --root "$WORK/pkg-$suffix" \
    --scripts "$(dirname "$0")/scripts" \
    --identifier "$IDENT" \
    --version "$VERSION" \
    --install-location /Applications \
    "dist/roamming_${VERSION}_darwin_${suffix}.pkg"
}

if [ -n "$BIN_AMD64" ]; then
  build_pkg amd64 "$BIN_AMD64"
fi
if [ -n "$BIN_ARM64" ]; then
  build_pkg arm64 "$BIN_ARM64"
fi

# universal: both slices lipo'd into one download for any Mac
if [ -n "$BIN_AMD64" ] && [ -n "$BIN_ARM64" ]; then
  lipo -create -output "$WORK/roamming" "$BIN_AMD64" "$BIN_ARM64"
  build_pkg universal "$WORK/roamming"
fi

echo "macOS packages built:"
ls -la dist/*.pkg
