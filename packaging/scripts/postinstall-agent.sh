#!/bin/sh
set -e

# Reload systemd unit files
if command -v systemctl > /dev/null 2>&1 && systemctl is-system-running > /dev/null 2>&1; then
    systemctl daemon-reload || true
fi
