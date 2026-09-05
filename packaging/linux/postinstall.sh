#!/bin/sh
# Runs as root after the package files are installed (deb postinst,
# rpm %post, arch post_install).
set -e

# Turn on the user-level autostart service for every user on the system.
# `--global` works without a user session: it creates the
# default.target.wants symlink in /etc/systemd/user. The service itself
# starts at each user's next login.
systemctl --global enable roamming.service >/dev/null 2>&1 || true

# Best-effort desktop/icon cache refresh; DEs also pick the files up on
# their next start.
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q /usr/share/applications 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -q -f -t /usr/share/icons/hicolor 2>/dev/null || true
fi

exit 0
