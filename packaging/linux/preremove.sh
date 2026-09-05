#!/bin/sh
# Runs as root before the package files are removed (deb prerm,
# rpm %preun, arch pre_remove).
set -e

systemctl --global disable roamming.service >/dev/null 2>&1 || true

exit 0
