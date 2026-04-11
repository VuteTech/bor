# Audit Log Forwarding

Bor records every state-changing operation (REST API calls and agent-reported file tamper events) as a structured audit event. Events are persisted to the database (visible in the web UI under **Audit Logs**) and, when configured, forwarded in real time to a remote syslog receiver using RFC 5424 framing.

Two wire formats are supported:

| Format | Standard | Use with |
|--------|----------|----------|
| **CEF** | ArcSight Common Event Format v25 | IBM QRadar, Micro Focus ArcSight, Splunk (TA-cef), most legacy SIEMs |
| **OCSF** | Open Cybersecurity Schema Framework v1.3.0 (JSON) | Microsoft Sentinel, Splunk OCSF app, AWS Security Lake, modern XDR platforms |

---

## Table of Contents

1. [Configuration](#configuration)
2. [CEF format reference](#cef-format-reference)
3. [OCSF format reference](#ocsf-format-reference)
4. [Reading with syslog-ng (dev / test)](#reading-with-syslog-ng-dev--test)
5. [Connecting to a SIEM](#connecting-to-a-siem)
6. [Secret redaction](#secret-redaction)
7. [Severity mapping](#severity-mapping)

---

## Configuration

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BOR_AUDIT_SYSLOG_ENABLED` | `false` | Set to `true` to activate forwarding |
| `BOR_AUDIT_SYSLOG_NETWORK` | `udp` | Transport: `udp`, `tcp`, or `tcp+tls` |
| `BOR_AUDIT_SYSLOG_ADDR` | `localhost:514` | Syslog receiver `host:port` |
| `BOR_AUDIT_SYSLOG_FORMAT` | `cef` | Message body format: `cef` or `ocsf` |
| `BOR_AUDIT_SYSLOG_FACILITY` | `16` | RFC 5424 facility (16 = local0) |
| `BOR_AUDIT_SYSLOG_TLS_CA` | _(empty)_ | PEM CA file for `tcp+tls` receiver verification; empty = system pool |

### YAML (`/etc/bor/server.yaml`)

```yaml
audit:
  syslog:
    enabled: true
    network: "tcp"           # udp | tcp | tcp+tls
    addr: "siem.corp:514"
    format: "cef"            # cef | ocsf
    facility: 16             # 0-23; 16 = local0
    tls_ca: ""               # PEM CA file path, only for tcp+tls
```

Environment variables always take precedence over the YAML file.

### Transport notes

- **UDP** — fire-and-forget; no delivery guarantee; lowest overhead. Suitable for high-trust LAN segments.
- **TCP** — connection-oriented with one retry on write failure. Use when delivery matters more than latency.
- **tcp+tls** — same as TCP with TLS 1.2+ encryption and server certificate verification. Required when forwarding over untrusted networks. Provide a CA cert file with `tls_ca` when using a private CA.

The syslog sink uses a 512-event internal buffer. Events are dispatched asynchronously so a slow or unreachable receiver never blocks HTTP request handling. When the buffer is full, excess events are dropped with a warning log line.

---

## CEF format reference

CEF (Common Event Format) encodes events as a pipe-delimited header followed by space-separated `key=value` extension fields, all on a single line. Bor wraps each event in an RFC 5424 syslog envelope.

### Full syslog line

```
<134>1 2026-04-11T10:23:45Z bor-server Bor - - - CEF:0|Vute Tech|Bor|1.0|create|policies create|3|rt=1744366225000 src=10.0.1.42 suser=alice cs1Label=resourceId cs1=7f3a1b2c-... cs2Label=resourceName cs2=Firefox ESR cs3Label=resourceType cs3=policies outcome=success requestMethod=POST request=/api/v1/policies/all msg=name\=Firefox ESR type\=firefox
```

### CEF header fields

| Position | Field | Example | Notes |
|----------|-------|---------|-------|
| 1 | Version | `CEF:0` | Always CEF version 0 |
| 2 | DeviceVendor | `Vute Tech` | |
| 3 | DeviceProduct | `Bor` | |
| 4 | DeviceVersion | `1.0` | |
| 5 | SignatureID | `create` | Bor action verb |
| 6 | Name | `policies create` | `{resource_type} {action}` |
| 7 | Severity | `3` | 0–10 scale (see [Severity mapping](#severity-mapping)) |

### CEF extension fields

| Key | Maps to | Example |
|-----|---------|---------|
| `rt` | Event time (epoch ms) | `1744366225000` |
| `src` | Client IP address | `10.0.1.42` |
| `suser` | Username (actor) | `alice` |
| `cs1Label` / `cs1` | Resource ID label + value | `resourceId` / `7f3a1b2c-…` |
| `cs2Label` / `cs2` | Resource name label + value | `resourceName` / `Firefox ESR` |
| `cs3Label` / `cs3` | Resource type label + value | `resourceType` / `policies` |
| `outcome` | `success` or `failure` | `success` |
| `requestMethod` | HTTP method (API events) | `POST` |
| `request` | HTTP path (API events) | `/api/v1/policies/all` |
| `msg` | Redacted request body or process list | `name=Firefox ESR type=firefox` |
| `filePath` | Tampered file path (tamper events) | `/etc/dconf/db/local.d/00-bor-lock` |

CEF extension values are escaped per the specification: `\` → `\\`, `=` → `\=`, newline → `\n`.

### Parsing CEF in syslog-ng

```
parser p_cef {
    csv-parser(
        columns("cef_version", "vendor", "product", "dev_version", "sig_id", "name", "severity", "extensions")
        delimiters("|")
        prefix(".cef.")
    );
    kv-parser(prefix(".cef.ext.") template("${.cef.extensions}"));
};
```

---

## OCSF format reference

OCSF (Open Cybersecurity Schema Framework) events are serialised as compact JSON and embedded as the message body of an RFC 5424 syslog line. Bor targets OCSF schema version **1.3.0**.

### OCSF class mapping

| Bor event type | OCSF class | class_uid |
|----------------|-----------|-----------|
| REST API: users resource | Account Change | 3002 |
| REST API: any other resource | API Activity | 6003 |
| Agent file tamper | File System Activity | 1001 |

### Full syslog line (pretty-printed for readability)

```
<134>1 2026-04-11T10:23:45Z bor-server Bor - - - {
  "class_uid": 6003,
  "class_name": "API Activity",
  "activity_id": 1,
  "activity": "Create",
  "category_uid": 6,
  "category_name": "Application Activity",
  "type_uid": 600301,
  "severity": "Low",
  "severity_id": 2,
  "time": 1744366225000,
  "message": "create policies 7f3a1b2c-...",
  "metadata": {
    "product": { "name": "Bor", "vendor_name": "Vute Tech", "version": "1.0" },
    "version": "1.3.0",
    "event_code": "create"
  },
  "actor": {
    "user": { "name": "alice", "uid": "a1b2c3d4-..." }
  },
  "src_endpoint": { "ip": "10.0.1.42" },
  "api": {
    "operation": "create",
    "request": {
      "method": "POST",
      "url": "/api/v1/policies/all",
      "body": "{\"name\":\"Firefox ESR\",\"type\":\"firefox\"}"
    }
  }
}
```

**Tamper event example** (`class_uid` 1001):

```json
{
  "class_uid": 1001,
  "class_name": "File System Activity",
  "activity_id": 3,
  "activity": "Modify",
  "severity": "High",
  "severity_id": 4,
  "message": "managed file tampered: /etc/dconf/db/local.d/00-bor",
  "file": {
    "path": "/etc/dconf/db/local.d/00-bor",
    "process": [
      { "pid": 4821, "name": "vim", "user": "root" }
    ]
  }
}
```

`type_uid` is always `class_uid × 100 + activity_id`, matching the OCSF specification.

---

## Reading with syslog-ng (dev / test)

The `podman-compose.yml` in this repository includes a **syslog-ng** service for local testing. It listens on UDP and TCP 514, prints every received message to stdout, and writes messages to `/var/log/syslog-ng/` inside the container.

### Start the stack with syslog

```bash
podman-compose up -d
```

### Watch incoming audit events in real time

```bash
# All syslog-ng output (includes Bor events)
podman-compose logs -f syslog-ng

# Filter to Bor events only
podman exec bor-syslog-ng tail -f /var/log/syslog-ng/bor.log
```

### Trigger a test event

```bash
# Log in to get a JWT token
TOKEN=$(curl -sk -X POST https://localhost:8443/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"change-me-in-production"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')

# Create a node group — this triggers an audit event
curl -sk -X POST https://localhost:8443/api/v1/node-groups \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"test-group","description":"Audit log test"}'
```

Within a second you should see a CEF line in `podman-compose logs syslog-ng`:

```
Apr 11 10:23:45 bor-server Bor - - - CEF:0|Vute Tech|Bor|1.0|create|node-groups create|3|rt=... suser=admin ...
```

### Switch to OCSF format

Edit `podman-compose.yml` (or set `BOR_AUDIT_SYSLOG_FORMAT=ocsf` in `.env`) and recreate the server container:

```bash
podman-compose up -d --no-deps server
```

---

## Connecting to a SIEM

### Splunk (Universal Forwarder / HEC)

Point the syslog-ng `d_splunk` destination at the Splunk HEC or syslog input. For the syslog input, configure the source type:

| Format | Splunk source type |
|--------|--------------------|
| CEF | `cef` (requires TA-cef add-on) |
| OCSF | `_json` with OCSF field extractions |

Recommended inputs.conf stanza for a Splunk Heavy Forwarder receiving TCP syslog:

```ini
[tcp://514]
sourcetype = cef
index = bor_audit
```

### IBM QRadar

Add a **Universal DSM** log source pointing at the Bor server IP on UDP/TCP 514. QRadar's Auto Detect should recognise CEF and map `suser` → username, `src` → source IP, `outcome` → event outcome automatically.

For OCSF, use a custom log source type with JSON parsing and map `actor.user.name`, `src_endpoint.ip`, and `action` to QRadar fields.

### Microsoft Sentinel

Use the **Common Event Format (CEF) via AMA** data connector for CEF, or the **Syslog via AMA** connector with a custom parser for OCSF. The recommended approach for OCSF is to ingest via the **OCSF Connector** or parse the JSON body in a KQL ingestion-time transformation:

```kql
// Example Sentinel KQL query for Bor OCSF events
CommonSecurityLog
| where DeviceVendor == "Vute Tech" and DeviceProduct == "Bor"
| extend Actor = tostring(parse_json(AdditionalExtensions).actor.user.name)
| extend Action = tostring(parse_json(AdditionalExtensions).activity)
```

### rsyslog (on-premise relay)

```
# /etc/rsyslog.d/50-bor.conf
module(load="imudp")
input(type="imudp" port="514")

# Write Bor events to a dedicated file
if $programname == 'Bor' then {
    action(type="omfile" file="/var/log/bor/audit.log")
    stop
}
```

---

## Secret redaction

Sensitive fields are redacted **before** the event reaches any sink (database, syslog, or future destinations). A field is considered sensitive when its lowercased key contains any of:

`password`, `passwd`, `secret`, `token`, `credential`, `passphrase`, `private_key`, `privatekey`, `api_key`, `apikey`, `jwt`, `auth_token`, `access_token`, `refresh_token`

Matching values are replaced with the literal string `[REDACTED]` in both the `msg` CEF extension field and the OCSF `api.request.body` field. The replacement happens in-process before the body bytes reach any I/O path — the original secret is never written to disk or transmitted over the network.

---

## Severity mapping

### CEF severity (0–10)

| Bor action | CEF severity | Label |
|------------|-------------|-------|
| `tamper_detected` | 8 | High |
| `delete` | 6 | Medium-high |
| `create`, `update` | 3 | Low |
| other | 1 | Informational |

### OCSF severity_id (0–5)

| Bor action | OCSF severity_id | Label |
|------------|-----------------|-------|
| `tamper_detected` | 4 | High |
| `delete` | 3 | Medium |
| `create`, `update` | 2 | Low |
| other | 1 | Informational |

### RFC 5424 syslog severity

| Bor action | Syslog severity | Code |
|------------|----------------|------|
| `tamper_detected` | Warning | 4 |
| `delete` | Notice | 5 |
| `create`, `update` | Informational | 6 |
| other | Informational | 6 |

The RFC 5424 priority byte embedded in `<PRI>` is `facility × 8 + syslog_severity`. With the default facility 16 (local0), a tamper event has priority `<132>` and an API create has priority `<134>`.
