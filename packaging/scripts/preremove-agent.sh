#!/bin/sh
set -e

if command -v systemctl > /dev/null 2>&1; then
    systemctl stop bor-agent 2>/dev/null || true
    systemctl disable bor-agent 2>/dev/null || true
fi
