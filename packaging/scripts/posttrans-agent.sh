#!/bin/sh
set -e

# rpm only: runs after every scriptlet of the transaction, so after the
# previous package's preremove. On an upgrade of an enrolled agent, keep
# the unit enabled and start it on the new binary.
if [ -f /run/bor-agent.upgrade ]; then
    rm -f /run/bor-agent.upgrade
    if command -v systemctl > /dev/null 2>&1 && systemctl is-system-running > /dev/null 2>&1 && [ -f /var/lib/bor/agent/agent.crt ]; then
        systemctl enable bor-agent 2>/dev/null || true
        systemctl restart bor-agent 2>/dev/null || true
    fi
fi
