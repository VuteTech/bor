#!/bin/sh
set -e

# Reload systemd unit files. On an upgrade the enrolled agent is kept
# enabled and restarted on the new binary: dpkg runs this last ("configure"
# with the previous version), rpm runs the old package's preremove after
# this, so the rpm upgrade leaves a marker for the posttrans script.
if command -v systemctl > /dev/null 2>&1 && systemctl is-system-running > /dev/null 2>&1; then
    systemctl daemon-reload || true
    case "${1:-}" in
        2)
            touch /run/bor-agent.upgrade 2>/dev/null || true
            ;;
        configure)
            if [ -n "${2:-}" ] && [ -f /var/lib/bor/agent/agent.crt ]; then
                systemctl enable bor-agent 2>/dev/null || true
                systemctl restart bor-agent 2>/dev/null || true
            fi
            ;;
    esac
fi
