<p align="center">
  <img src="docs/design/logo.png" alt="roamming logo" width="400">
</p>

# roamming

A system-tray desktop app (Go + [Fyne](https://fyne.io)) that sets **your own**
external activity indicator in [Roam](https://ro.am) via the
[`user.activity` API](https://developer.ro.am/docs/guides/user-activity):
a badge, tooltip text, glow color, optional Do-Not-Disturb and an optional
timer.

**Only your permissions, ever.** The app authenticates with a *personal* OAuth
grant and requests exactly two scopes:

- `user:write.activity`: set/clear your activity
- `user:read.activity`: list your live activities (all integrations)

It can never target another user: the API's personal access model restricts
every call to the token owner.

## Gallery

The UI follows Roam's own design language: near-black layered surfaces,
rounded cards, pill buttons, muted secondary text and the lime accent,
set in bundled Inter.

| Connect | Set an activity |
| --- | --- |
| ![Connect screen](docs/gallery/connect.png) | ![Activity editor](docs/gallery/activity.png) |

Tray states (idle, running, error):

<p align="center">
  <img src="docs/gallery/tray-states.png" alt="Tray icon states: idle, running, error">
</p>

These are rendered from the real UI by an offscreen test, so they stay in
sync with the code. Regenerate after UI changes with:

```sh
ROAMMING_CAPTURE_GALLERY=1 go test ./internal/ui -run TestCaptureGallery
```

## Install

Direct downloads pull the latest release automatically:

<!-- The Windows logo is inlined as a data URI: simple-icons removed
     all Microsoft marks after a legal request, so logo=windows no
     longer renders anything. -->

| Linux | macOS | Windows |
| :---: | :---: | :---: |
| [![Ubuntu](https://img.shields.io/badge/Ubuntu-E95420?style=for-the-badge&logo=ubuntu&logoColor=white)](https://github.com/nathabonfim59/roamming/releases/latest/download/roamming_linux_amd64.deb)<br>[![Red Hat](https://img.shields.io/badge/Red_Hat-EE0000?style=for-the-badge&logo=redhat&logoColor=white)](https://github.com/nathabonfim59/roamming/releases/latest/download/roamming_linux_x86_64.rpm)<br>[![Arch Linux](https://img.shields.io/badge/Arch_Linux-1793D1?style=for-the-badge&logo=archlinux&logoColor=white)](https://github.com/nathabonfim59/roamming/releases/latest/download/roamming_linux_x86_64.pkg.tar.zst) | [![macOS](https://img.shields.io/badge/macOS-000000?style=for-the-badge&logo=apple&logoColor=white)](https://github.com/nathabonfim59/roamming/releases/latest/download/roamming_darwin_universal.pkg) | [![Windows](https://img.shields.io/badge/Windows-0078D4?style=for-the-badge&logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0iI2ZmZiI%2BPHBhdGggZD0iTTAgMy40NDlMOS43NSAyLjF2OS40NTFIMG0xMC45NDktOS42MDJMMjQgMHYxMS40SDEwLjk0OU0wIDEyLjZoOS43NXY5LjQ1MUwwIDIwLjY5OU0xMC45NDkgMTIuNkgyNFYyNGwtMTIuOS0xLjgwMSIvPjwvc3ZnPg%3D%3D&logoColor=white)](https://github.com/nathabonfim59/roamming/releases/latest/download/roamming_windows_amd64_setup.exe) |

Every installer registers roamming to start when you log in, so the tray
app can keep your activity current. The macOS button is a **universal**
build (Intel + Apple Silicon); arch-specific packages are on the
[releases page](https://github.com/nathabonfim59/roamming/releases/latest).

Details:

| Platform | File | Installs to | Autostart |
| --- | --- | --- | --- |
| Debian/Ubuntu | `roamming_*_amd64.deb` | `/usr/bin/roamming` | systemd user unit (enabled via `systemctl --global`) |
| RHEL/Fedora | `roamming-*.x86_64.rpm` | `/usr/bin/roamming` | same |
| Arch Linux | `roamming-*-x86_64.pkg.tar.zst` | `/usr/bin/roamming` | same |
| macOS | `roamming_*_darwin_*.pkg` | `/Applications/roamming.app` | LaunchAgent (`/Library/LaunchAgents`) |
| Windows | `roamming_*_windows_amd64_setup.exe` | `%LOCALAPPDATA%\Programs\roamming` | `HKCU\...\Run` (opt-out checkbox in the installer) |

To turn autostart off again:

- **Linux**: `systemctl --global disable roamming.service`
- **macOS**: remove `/Library/LaunchAgents/com.nathabonfim59.roamming.plist`,
  or untick roamming under System Settings → General → Login Items
- **Windows**: untick *Start roamming when I log in* during install, remove
  the `roamming` value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
  or disable it in Task Manager → Startup apps

The Linux package also ships a `.desktop` entry and hicolor icons, so
roamming shows up in app launchers. The bare archives (`.tar.gz`/`.zip`)
stay available for portable use; they register nothing.

## Setup

1. Register an OAuth app in Roam: **Administration → Developer → Add ApiClient**
   (authorization type **OAuth**) with:
   - Redirect URI (exact match): `http://127.0.0.1:18079/callback`
   - Scopes: `user:write.activity`, `user:read.activity`
2. Copy the **Client ID** (and **Client Secret** only if you registered a
   confidential client; public/PKCE clients leave it empty).
3. Run the app, paste the Client ID, hit **Connect with Roam**.

The app starts a **temporary, stdlib-only `net/http` server** on
`127.0.0.1:18079`, opens your browser at `https://ro.am/oauth/authorize`
(authorization-code flow, **PKCE S256**, CSRF `state` check), captures the
redirect, exchanges the code, resolves your identity via `token.info`, and
shuts the server down. Access tokens are refreshed automatically (7-day
lifetime, rotated refresh tokens); a revoked grant sends you back to the
connect screen.

## Build

Requires Go 1.24+ and the usual Fyne native dependencies:

```sh
# Debian/Ubuntu
sudo apt install build-essential pkg-config libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev

go build -o roamming .
```

### Release

Pushing a `v*` tag runs the release workflow on GitHub Actions: each OS
builds natively in a matrix (linux/amd64, darwin/amd64+arm64,
windows/amd64) and produces the archives *plus* installers —
deb/rpm/Arch packages, macOS `.pkg` installers (native `pkgbuild`), and
a Windows NSIS setup (`makensis`; GoReleaser's own NSIS support is
Pro-only). The publish job merges everything into a single GitHub
release with checksums over all artifacts.

A manual `workflow_dispatch` run of the Release workflow builds all
installers without publishing — a dry run to exercise the pipeline
before tagging. Locally, the Linux packages can be exercised with:

```sh
goreleaser release --snapshot --clean -f .goreleaser/linux.yaml
```

The goreleaser configs live in `.goreleaser.yaml` (master view) and
`.goreleaser/<os>.yaml` (what CI actually runs per OS).

## Usage

- **Tray icon**: left-click opens the window, right-click opens the menu
  (status preview with live countdown, Start/Stop, Show window, Quit).
  Closing the window hides it; **Quit** clears the activity first.
- **Presets**: On a call, In a meeting, Focusing, … pre-fill the form.
- **Emoji**: pick from a curated grid or paste any emoji (≤ 16 code points).
- **Text**: status title (required) and subtitle (optional), ≤ 140 code points.
- **Glow color**: the 12 curated Roam palette names; *none* = quiet badge.
- **DND**: optionally locks your assigned office while the activity runs.
- **Timer (optional)**: 15 min … 8 h, or run until you stop it.

### End-time text: fixed or live countdown

Two user-selectable styles for timed activities:

- **Fixed end time** (default): the title gets `… · back at 15:04 BRT`
  (your local timezone), computed once at start. A timer within the
  server's 1-hour TTL posts **exactly once**; nothing re-posts until the
  final clear.
- **Live countdown**: the title gets `… · back in 24m`, re-posted every
  minute so the badge text ticks on the Roam map.

Open-ended activities (no timer) heartbeat every 15 minutes within a
30-minute TTL: deliberately few posts, at the cost of a badge that can
linger up to 30 minutes if the app dies without clearing. Timers longer
than 1 hour heartbeat every 45 minutes (the server clamps TTLs to one
hour). Stopping or quitting always clears the activity; a missed clear
also expires server-side within the TTL.

## Files & security

- The OAuth grant (access + refresh tokens) lives in the **OS keychain**
  as a single secret (`com.nathabonfim59.roamming`): macOS Keychain,
  Windows Credential Manager, or a freedesktop.org Secret Service
  (gnome-keyring/KWallet) on Linux. It never touches disk in plaintext;
  the keychain's own unlock policy is the trust boundary.
- Upgrades from roamming <= 1.1.3 migrate automatically: the old
  `credentials.json` is moved into the keychain and deleted on first
  launch (kept only if no keychain is available).
- Last-used activity settings live in Fyne preferences.

## Layout

```
main.go                    app bootstrap
internal/roam/             API client, OAuth+PKCE+callback server, token store
internal/session/          activity lifecycle: set, heartbeats, timer, clear
internal/ui/               Fyne window, pickers, palette, tray icons
```

The embedded app icon (`internal/ui/appicon.png`) is a copy of
`docs/design/favicon.png`; after changing the artwork, run
`make update-logo` and rebuild.
