# Automatic HTTPS certificates (ACME / Let's Encrypt)

Bor supports automatic TLS certificate provisioning via the **ACME protocol (RFC 8555)**. When enabled, the server obtains and renews a publicly-trusted certificate from any ACME-compatible certificate authority — Let's Encrypt, ZeroSSL, Buypass, Google Trust Services, or a private CA that speaks ACME.

ACME applies only to the **UI and enrollment port** (default `:8443`, the one browsers and the admin connect to). The **agent mTLS port** (`:8444`) always uses the internal CA-signed certificate so enrolled agents can verify it with the CA cert they received during enrollment.

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick start — Let's Encrypt](#quick-start--lets-encrypt)
3. [Configuration reference](#configuration-reference)
4. [Supported ACME CAs](#supported-acme-cas)
5. [Challenge methods](#challenge-methods)
6. [Reverse-proxy setups](#reverse-proxy-setups)
7. [Certificate cache and renewal](#certificate-cache-and-renewal)
8. [Troubleshooting](#troubleshooting)

---

## Prerequisites

- A **public domain name** (`bor.example.com`) with a DNS A/AAAA record pointing to the server's public IP.
- The ACME CA must be able to reach the server on **port 80** (HTTP-01 challenge) or **port 443** (TLS-ALPN-01). See [Reverse-proxy setups](#reverse-proxy-setups) if the server listens on a non-standard port.
- **`BOR_TLS_CERT_FILE` / `BOR_TLS_KEY_FILE` must not be set** — ACME and explicit cert files are mutually exclusive.

---

## Quick start — Let's Encrypt

Add to `.env` (or `server.yaml`):

```bash
BOR_ACME_ENABLED=true
BOR_ACME_DOMAINS=bor.example.com
BOR_ACME_EMAIL=admin@example.com
```

Start the server. On the first request to `https://bor.example.com:8443`, Bor will automatically:

1. Register an ACME account with Let's Encrypt.
2. Respond to the HTTP-01 challenge on port 80.
3. Obtain a certificate and cache it in `/var/lib/bor/acme/`.
4. Serve the certificate for all subsequent TLS handshakes.
5. Renew the certificate automatically before it expires (Let's Encrypt certificates are valid 90 days; renewal begins at 30 days remaining).

> **Staging first**: use `BOR_ACME_DIRECTORY=https://acme-staging-v02.api.letsencrypt.org/directory` during testing to avoid hitting Let's Encrypt's [rate limits](https://letsencrypt.org/docs/rate-limits/). Staging certificates are not trusted by browsers but are otherwise identical in behaviour.

---

## Configuration reference

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BOR_ACME_ENABLED` | `false` | Enable ACME certificate provisioning |
| `BOR_ACME_DOMAINS` | _(none)_ | Comma-separated list of domains (required when enabled) |
| `BOR_ACME_EMAIL` | _(none)_ | Contact email registered with the ACME CA (required) |
| `BOR_ACME_DIRECTORY` | Let's Encrypt production | ACME directory URL of the CA |
| `BOR_ACME_CACHE_DIR` | `/var/lib/bor/acme` | Directory for ACME account keys and certificates |
| `BOR_ACME_HTTP_PORT` | `80` | Local port for the HTTP-01 challenge listener |

### YAML (`/etc/bor/server.yaml`)

```yaml
acme:
  enabled: true
  domains:
    - bor.example.com
  email: admin@example.com
  # directory_url defaults to Let's Encrypt production when omitted
  # directory_url: https://acme-v02.api.letsencrypt.org/directory
  # cache_dir: /var/lib/bor/acme
  # http_port: 80
```

Environment variables always take precedence over the YAML file.

---

## Supported ACME CAs

Any RFC 8555-compliant certificate authority is supported. Set `BOR_ACME_DIRECTORY` to the CA's directory URL:

| CA | Directory URL | Notes |
|----|--------------|-------|
| **Let's Encrypt** (production) | `https://acme-v02.api.letsencrypt.org/directory` | Default; free, browser-trusted |
| **Let's Encrypt** (staging) | `https://acme-staging-v02.api.letsencrypt.org/directory` | For testing; not browser-trusted |
| **ZeroSSL** | `https://acme.zerossl.com/v2/DV90` | Free DV; requires EAB credentials in some configurations |
| **Buypass Go SSL** | `https://api.buypass.com/acme/directory` | Free DV; 180-day certificates |
| **Google Trust Services** | `https://dv.acme-v02.api.pki.goog/directory` | Requires a Google Cloud account |
| **Step CA** (private) | `https://ca.internal/acme/acme/directory` | Self-hosted; for internal-only deployments |

---

## Challenge methods

Two challenge types are wired up automatically:

### HTTP-01 (default, recommended)

Bor starts a plain-HTTP listener on `BOR_ACME_HTTP_PORT` (default `80`). The ACME CA sends a GET request to:

```
http://<domain>/.well-known/acme-challenge/<token>
```

**Requirements:**
- Port 80 must be reachable from the ACME CA's validation servers (i.e., the internet for Let's Encrypt).
- No firewall blocking inbound TCP/80.

### TLS-ALPN-01

Supported automatically when the TLS port is reachable as port 443 from the CA's perspective. Bor includes the `acme-tls/1` ALPN protocol in the TLS negotiation so the CA can validate ownership via a special TLS handshake.

**Requirements:**
- The server's TLS port must be reachable on port 443 externally. This works out of the box when `BOR_ENROLLMENT_PORT=443`, or when a reverse proxy forwards external 443 to the Bor TLS port.
- No additional configuration needed — Bor advertises the ALPN protocol automatically when ACME is enabled.

---

## Reverse-proxy setups

### Port 80 forwarding (HTTP-01 behind a reverse proxy)

If Bor runs on a non-standard port and a reverse proxy (nginx, Caddy, HAProxy) handles external port 80, configure the proxy to forward `/.well-known/acme-challenge/` to Bor's HTTP challenge listener.

**nginx example:**

```nginx
server {
    listen 80;
    server_name bor.example.com;

    # Forward ACME HTTP-01 challenges to Bor
    location /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:8080;  # BOR_ACME_HTTP_PORT=8080
    }

    # Redirect everything else to HTTPS
    location / {
        return 301 https://$host$request_uri;
    }
}
```

Set `BOR_ACME_HTTP_PORT=8080` (any unprivileged port) and ensure `127.0.0.1:8080` is reachable from the proxy.

### Running on port 443 directly

If Bor binds directly to port 443 (set `BOR_ENROLLMENT_PORT=443`), the TLS-ALPN-01 challenge works without a separate HTTP listener. HTTP-01 also works if port 80 is open.

---

## Certificate cache and renewal

Certificates, account keys, and ACME state are stored in `BOR_ACME_CACHE_DIR` (default `/var/lib/bor/acme/`). The directory is created with permissions `0700` on startup.

The files stored there include:
- `acme_account+key` — ACME account private key (keep this backed up)
- `<domain>` — issued certificate and private key for each domain

**Renewal** is handled automatically by the Go `autocert` library:
- Let's Encrypt certificates are valid for 90 days.
- Renewal is initiated when fewer than 30 days remain.
- A renewed certificate is served immediately on the next TLS handshake; no restart required.

**Backup**: back up the cache directory. Losing the account key means re-registering with the CA on next startup (new account, new certificates).

---

## Troubleshooting

### "too many registrations for this IP" (Let's Encrypt)

You have hit Let's Encrypt's registration rate limit. Use the staging directory for testing:

```bash
BOR_ACME_DIRECTORY=https://acme-staging-v02.api.letsencrypt.org/directory
```

### Certificate not issued — HTTP challenge fails

1. Confirm port 80 is open: `curl http://bor.example.com/.well-known/acme-challenge/test`
2. Check `BOR_ACME_HTTP_PORT` matches what is actually reachable by the CA.
3. Check firewall rules: `sudo ss -tlnp | grep :80`
4. Check server logs for "ACME HTTP-01 challenge listener" startup message.

### "x509: certificate signed by unknown authority" in browsers

You are using the Let's Encrypt staging directory. Switch to production:

```bash
BOR_ACME_DIRECTORY=https://acme-v02.api.letsencrypt.org/directory
# Delete the cache to force re-registration
rm -rf /var/lib/bor/acme/
```

### Agent connection errors after enabling ACME

Agents connect to the **agent mTLS port** (`:8444`), which always uses the internal CA-signed certificate. ACME does not affect agent connectivity. If agents fail to connect, check:
- `BOR_POLICY_PORT` is correct in the agent config.
- The internal CA certificate has not been rotated (agents need the CA cert they received during enrollment).

### Mutual exclusion error on startup

```
BOR_ACME_ENABLED and BOR_TLS_CERT_FILE/BOR_TLS_KEY_FILE are mutually exclusive
```

Remove `BOR_TLS_CERT_FILE` and `BOR_TLS_KEY_FILE` from your configuration when using ACME.
