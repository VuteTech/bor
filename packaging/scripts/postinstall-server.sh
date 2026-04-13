#!/bin/sh
# Bor server post-install script.
# Runs after every install and upgrade.
set -e

# ── System user/group ──────────────────────────────────────────────────────────
if ! getent group bor > /dev/null 2>&1; then
    if command -v groupadd > /dev/null 2>&1; then
        groupadd --system bor
    elif command -v addgroup > /dev/null 2>&1; then
        addgroup -S bor
    fi
fi

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

# ── Permissions ────────────────────────────────────────────────────────────────
chown bor:bor /var/lib/bor/pki  2>/dev/null || true
chmod 0750    /var/lib/bor/pki  2>/dev/null || true
chown bor:bor /var/lib/bor/acme 2>/dev/null || true
chmod 0750    /var/lib/bor/acme 2>/dev/null || true

# server.yaml contains the JWT secret and optionally the DB password.
# Restrict access to root (write) and the bor service account (read).
chown root:bor /etc/bor/server.yaml 2>/dev/null || true
chmod 0640     /etc/bor/server.yaml 2>/dev/null || true

# ── Detect fresh install vs upgrade ───────────────────────────────────────────
# In RPM $1==1 means first install; in Debian $1=="configure" and $2 is empty
# on first install, set on upgrade.  Alpine and Arch pass no arguments.
_is_fresh_install() {
    case "${1:-}" in
        configure) [ -z "${2:-}" ] ;;  # Debian/Ubuntu
        1)         return 0 ;;         # RPM
        *)         return 0 ;;         # Alpine, Arch — treat as fresh install
    esac
}

if _is_fresh_install "$@"; then

    # ── Generate a random JWT secret ──────────────────────────────────────────
    # Replace the placeholder value in server.yaml so the service starts
    # securely without requiring manual configuration.
    _jwt=$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -A n -t x1 | tr -d ' \n')
    if [ -n "$_jwt" ]; then
        sed -i "s|jwt_secret: \"change-me-in-production\"|jwt_secret: \"${_jwt}\"|" \
            /etc/bor/server.yaml 2>/dev/null || true
    fi

    # ── Generate a random initial admin password ───────────────────────────────
    # Written to server.yaml so the server uses it on first startup when no
    # users exist.  The password is also printed to the console for the admin.
    _admin_pw=$(dd if=/dev/urandom bs=16 count=1 2>/dev/null | od -A n -t x1 | tr -d ' \n')
    if [ -n "$_admin_pw" ]; then
        # Append the setting under the security section (after admin_token line).
        sed -i "s|#admin_password: \"\"|admin_password: \"${_admin_pw}\"|" \
            /etc/bor/server.yaml 2>/dev/null || true
        echo ""
        echo "bor-server: ======================================================"
        echo "bor-server:  Initial admin credentials"
        echo "bor-server:    Username: admin"
        echo "bor-server:    Password: ${_admin_pw}"
        echo "bor-server:  Change this password immediately after first login."
        echo "bor-server: ======================================================"
        echo ""
    fi

    # ── PostgreSQL setup ───────────────────────────────────────────────────────
    # Locate the postgres superuser account (varies by distro)
    _pg_superuser=""
    for _u in postgres pgsql; do
        if getent passwd "$_u" > /dev/null 2>&1; then
            _pg_superuser="$_u"
            break
        fi
    done

    # Locate the PostgreSQL Unix socket directory
    _pg_socket=""
    for _d in /var/run/postgresql /tmp; do
        if [ -d "$_d" ]; then
            _pg_socket="$_d"
            break
        fi
    done

    if [ -n "$_pg_superuser" ] && [ -n "$_pg_socket" ]; then
        # Check if PostgreSQL is accepting connections
        if su -s /bin/sh "$_pg_superuser" -c \
            "psql -h '$_pg_socket' -c '' postgres" > /dev/null 2>&1; then

            # Create the 'bor' role if it does not exist
            su -s /bin/sh "$_pg_superuser" -c \
                "psql -h '$_pg_socket' -tc \
                    \"SELECT 1 FROM pg_roles WHERE rolname='bor'\" postgres \
                 | grep -q 1 || psql -h '$_pg_socket' -c \
                    \"CREATE ROLE bor LOGIN\" postgres" 2>/dev/null || true

            # Create the 'bor' database if it does not exist
            su -s /bin/sh "$_pg_superuser" -c \
                "psql -h '$_pg_socket' -tc \
                    \"SELECT 1 FROM pg_database WHERE datname='bor'\" postgres \
                 | grep -q 1 || psql -h '$_pg_socket' -c \
                    \"CREATE DATABASE bor OWNER bor\" postgres" 2>/dev/null || true

            # Patch server.yaml: set the socket path as the DB host
            sed -i \
                "s|host: \"/var/run/postgresql\"|host: \"${_pg_socket}\"|" \
                /etc/bor/server.yaml 2>/dev/null || true

            echo "bor-server: PostgreSQL role and database 'bor' configured."
            echo "bor-server: Using local socket at ${_pg_socket} (peer auth, no password)."
        else
            echo ""
            echo "bor-server: PostgreSQL is not running. Please:"
            echo "  1. Start PostgreSQL and create the role and database:"
            echo "       sudo -u postgres psql -c \"CREATE ROLE bor LOGIN PASSWORD 'change-me';\""
            echo "       sudo -u postgres psql -c \"CREATE DATABASE bor OWNER bor;\""
            echo "  2. Update /etc/bor/server.yaml:"
            echo "       database:"
            echo "         host: \"db-host-or-socket\""
            echo "         password: \"change-me\"   # omit for socket/peer auth"
            echo "         sslmode: \"require\"       # for TCP; use \"disable\" for socket"
            echo ""
        fi
    else
        echo ""
        echo "bor-server: PostgreSQL not found. Install it, create the role and database,"
        echo "  then update the database section in /etc/bor/server.yaml."
        echo ""
    fi

fi  # end fresh-install block

# ── Systemd ────────────────────────────────────────────────────────────────────
if command -v systemctl > /dev/null 2>&1 && systemctl is-system-running > /dev/null 2>&1; then
    systemctl daemon-reload || true
fi
