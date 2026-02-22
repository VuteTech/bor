#!/bin/sh
set -e

# Create bor system group
if ! getent group bor > /dev/null 2>&1; then
    if command -v groupadd > /dev/null 2>&1; then
        groupadd --system bor
    elif command -v addgroup > /dev/null 2>&1; then
        addgroup -S bor
    fi
fi

# Create bor system user
if ! getent passwd bor > /dev/null 2>&1; then
    if command -v useradd > /dev/null 2>&1; then
        useradd \
            --system \
            --gid bor \
            --home-dir /var/lib/bor \
            --no-create-home \
            --shell /sbin/nologin \
            --comment "Bor Policy Server" \
            bor
    elif command -v adduser > /dev/null 2>&1; then
        adduser -S -G bor -H -h /var/lib/bor -s /sbin/nologin bor
    fi
fi

# Set ownership of PKI data directory
chown bor:bor /var/lib/bor/pki 2>/dev/null || true
chmod 0750 /var/lib/bor/pki 2>/dev/null || true

# Protect the config file (contains DB password and JWT secret)
chown root:bor /etc/bor/server.env 2>/dev/null || true
chmod 0640 /etc/bor/server.env 2>/dev/null || true

# Reload systemd unit files
if command -v systemctl > /dev/null 2>&1 && systemctl is-system-running > /dev/null 2>&1; then
    systemctl daemon-reload || true
fi
