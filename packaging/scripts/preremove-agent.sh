#!/bin/sh
set -e

# Stop and disable the agent only when the package is removed. On an
# upgrade (rpm passes 1, dpkg "upgrade", "deconfigure" or
# "failed-upgrade") the unit stays enabled and the postinstall restarts
# it; apk and pacman call this script on removal only.
case "${1:-}" in
    1|upgrade|deconfigure|failed-upgrade)
        exit 0
        ;;
esac

if command -v systemctl > /dev/null 2>&1; then
    systemctl stop bor-agent 2>/dev/null || true
    systemctl disable bor-agent 2>/dev/null || true
fi
