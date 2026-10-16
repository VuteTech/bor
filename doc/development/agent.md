# Agent internals

This page describes how `bor-agent` works inside: start-up, enrollment and
renewal, the streaming loop, how each policy type is synced and merged,
compliance reporting, user notifications, tamper protection with the file
watcher, and the system tools the agent calls. Read it before adding or
changing a policy type; the step-by-step checklist for that is in
[Adding a policy type](adding-a-policy-type.md).

## Layout

| Path | Contents |
|---|---|
| `agent/cmd/agent/main.go` | `main`, the streaming loop, policy caches, every `syncAll*` function, file watcher hooks |
| `agent/cmd/agent/disk_encryption.go` | Disk-encryption sync, inventory reporting and the periodic task ticker |
| `agent/cmd/agent/luks_cmd.go` | The `bor-agent luks` subcommand |
| `agent/cmd/gen-dconf-schemas`, `agent/cmd/gen-polkit-actions` | Generators for the server's built-in dconf schema and polkit action catalogues |
| `agent/internal/config` | YAML configuration loader and defaults |
| `agent/internal/policyclient` | gRPC client, enrollment (token and Kerberos), certificate renewal |
| `agent/internal/policy` | Per-type rendering, merging, writing and compliance checks |
| `agent/internal/filewatcher` | inotify-based tamper detection |
| `agent/internal/luks` | LUKS2 inventory, keyslots, recovery keys, TPM2 and Tang via Clevis, initramfs |
| `agent/internal/sessionaccess`, `agent/internal/logind` | Session access enforcer and logind lock watching |
| `agent/internal/notify` | Desktop notifications and KDE reconfigure over D-Bus |
| `agent/internal/procinfo` | Find processes holding a file open (tamper attribution) |
| `agent/internal/sysinfo` | Node metadata for heartbeats |

## Configuration

`config.Load(path)` (`agent/internal/config/config.go`) starts from
`DefaultConfig()`, reads the YAML file (default `/etc/bor/config.yaml`,
override with `--config`) and overlays it. A missing or unparsable file is
fatal. If `agent.client_id` is empty, the host name is used; it becomes the
requested node name at enrollment.

The defaults hold every managed path, for example
`/etc/firefox/policies/policies.json`, `/etc/opt/chrome/policies/managed`,
`/etc/firewalld/zones`, `/etc/bor/xdg` for KConfig, `/etc/bor/flatpak` and
`/var/lib/bor/agent` as `enrollment.data_dir`. Code should always read
paths from `cfg`, never hard-code them, so tests and unusual distributions
can override them.

## Start-up

`main()` runs in this order:

1. Parse `--config`, `--token`, `--token-file`. If the first argument is
   `luks`, run the subcommand and exit (see [below](#the-luks-subcommand)).
2. Resolve the enrollment token: `--token-file`, then
   `BOR_ENROLLMENT_TOKEN`, then the deprecated `--token`.
3. Load the configuration.
4. **Enrollment.** If a token is given and the agent is already enrolled,
   delete the old credentials first (re-enrollment). If not enrolled, try
   Kerberos enrollment when `kerberos.enabled` is set and the keytab exists,
   otherwise token enrollment. On success print the next steps and
   `os.Exit(0)`: enrollment is a one-shot command, the daemon is started by
   systemd afterwards.
5. **Renewal check.** If `agent.crt` expires within 30 days, call
   `policyclient.RenewCertificate` on the policy port. A failure is logged
   and the agent continues with the current certificate.
6. Create the mTLS client with `ca.crt`, `agent.crt` and `agent.key` from
   the data directory (TLS 1.3, verification always on).
7. Install `SIGINT` / `SIGTERM` handling, start the file watcher, the
   session access enforcer (if `loginctl` exists), detect the package
   manager, connect to the system D-Bus for PackageKit, and probe `flatpak`
   and the disk-encryption tools so the journal explains later
   `INAPPLICABLE` results.
8. Enter `runStreamingLoop`.

## The streaming loop

`runStreamingLoop` keeps `lastRevision` in memory and loops until the
context is cancelled. On each connection attempt it:

1. Fetches `GetAgentConfig` and rebuilds the notification settings.
2. Starts background goroutines that report the LUKS inventory and send a
   heartbeat, and, once per process, scan GSettings schemas and polkit
   actions and report the catalogues.
3. Calls `client.SubscribePolicyUpdates(ctx, lastRevision, callback)`. The
   callback stores the revision of every event except `METADATA_REQUEST`
   and `DISK_ENCRYPTION_TASK`, then calls `handlePolicyUpdate`.
4. When the stream ends, waits and reconnects:

```go
// Exponential backoff capped at 60 s.
backoff *= 2
if backoff > 60*time.Second {
    backoff = 60 * time.Second
}
```

The first retry waits 1 second. The backoff is not reset after a
successful connection, so once a few disconnects have happened, later
reconnects wait up to 60 seconds.

### Handling updates

`handlePolicyUpdate` keeps one cache per policy type (`firefoxCache`,
`dconfCache`, `polkitCache`, ...), keyed by policy ID:

- **`SNAPSHOT`**: entries are added to per-type staging maps. When the
  message with `snapshot_complete` arrives, each staging map replaces its
  cache, and every type is re-synced. An empty snapshot clears all caches
  and syncs, which removes everything Bor manages. For the browser and
  KConfig types the agent compares the new cache with the old one so that a
  resync without changes does not trigger user notifications.
- **`CREATED` / `UPDATED`**: update one cache entry and re-sync that type.
- **`DELETED`**: remove the entry from whichever cache holds it and re-sync
  that type.
- **`METADATA_REQUEST`**: send a heartbeat.
- **`DISK_ENCRYPTION_TASK`**: fetch and run disk-encryption tasks.
- An unknown policy type is reported as non-compliant with
  `unsupported policy type: <type>`.

Types whose apply step can take long (packages, Flatpak, disk encryption)
run in goroutines through `triggerPackageSync`, `triggerFlatpakSync` and
`triggerDiskEncryptionSync`, serialized by their own mutex, so they never
block the stream.

## Per-type sync functions

Each type has a `syncAll*` function in `main.go` that turns the whole cache
for that type into the desired state on disk. They are idempotent and are
called after every change, after every snapshot and by tamper restore.

| Function | Writes / does | Merge |
|---|---|---|
| `syncAllFirefox`, `syncAllThunderbird` | `policies.json` (system and Flatpak extension paths); original file backed up on first write and restored when no policies remain | `proto.Merge` of all cached policies |
| `syncAllChrome`, `syncAllEdge` | `bor_managed.json` in each managed policy directory | proto merge |
| `syncAllKConfig` | INI files under `/etc/bor/xdg` with `[$i]` immutability markers, the `XDG_CONFIG_DIRS` hook scripts, optionally `/etc/kde5rc` | `MergeKConfigPolicies`: sorted by priority descending (ties by ID), first writer wins per `(file, group, key)`; losers reported as inapplicable items |
| `syncAllDConf` | keyfile `00-bor` and a locks file in `/etc/dconf/db/<db>.d`, then `dconf update` | sorted by priority ascending, last writer wins per `(schema, path, key)` |
| `syncAllPolkit` | one rules file per policy in `/etc/polkit-1/rules.d`, file name derived from priority and policy ID | separate files, ordered by priority |
| `syncAllFirewalld` | zone XML in `/etc/firewalld/zones`, `firewall-cmd --check-config` and `--reload` | rules grouped per target zone (`SyncFirewalldFromProto`) |
| `syncAllSessionAccess` | `/etc/security/time.conf` lines and the PAM integration; feeds the logind enforcer | rules from all policies, merged into the existing file (`MergeTimeConf`) |
| `syncAllPackages` | `bor-*` repository and key files, then installs / removes through PackageKit | sorted by priority |
| `syncAllFlatpak` | remote definitions in `/etc/bor/flatpak`, then `flatpak` CLI operations; optional auto-update ticker | `MergeFlatpakEntries` |
| disk encryption (`disk_encryption.go`) | LUKS keyslots, Clevis bindings, crypttab options, dracut drop-in and initramfs rebuilds | `luks` package |

!!! warning "Merge order for map-based caches"
    The Firefox, Thunderbird, Chrome, Edge and KConfig caches store only the
    typed policy, not its priority, and are merged in Go map iteration
    order. When two such policies set the same key, the winner is not
    defined. dconf, polkit and packages carry the priority and sort by it.
    New types should store the priority in the cache entry and sort before
    merging.

## Compliance reporting

Every `syncAll*` ends by reporting compliance for each policy it handled:

- `client.ReportComplianceWithStatus(ctx, id, status, message, items)` for
  the four-state status and optional per-item results (dconf keys, polkit
  rules, packages, Flatpak apps).
- `client.ReportCompliance(ctx, id, compliant, message)` for the simple
  boolean case (the browser types report `Deployed` after a successful
  write).

Use `INAPPLICABLE` when the feature is not present on the node (no
PackageKit, no `flatpak`, GSettings schema not installed) and `ERROR` when
it is present but enforcement or the check failed. Both calls use a
10-second timeout and are best effort; a failed report is not retried
until the next sync.

## Notifications

When content actually changes, the agent can tell logged-in users. Each
type has a `notify.Notifier` (`kdeNotifier`, `firefoxNotifier`,
`chromeNotifier`, ...) and a `notify.Config` built from `GetAgentConfig`
(`notify_users`, cooldown, per-browser messages). `ScheduleNotification`
debounces for 10 seconds (`notify.NotifyDebounce`) so a batch of admin
edits produces one notification.

Because the agent runs as root and the session bus rejects root, D-Bus calls
are executed as subprocesses with the target user's UID and GID. For
KConfig the notifier also sends the KDE reconfigure signal so running
applications pick up the new settings. The session access enforcer uses the
same mechanism (`notify/alert.go`) for per-user schedule warnings.

## Tamper protection: the file watcher

Every policy type that writes files must be protected against local edits.
`agent/internal/filewatcher` watches the **parent directories** of managed
files with fsnotify, so atomic renames are seen, and calls the tamper
callback on write, create (rename into place) and remove events for a
managed path. Events for paths that were recently suppressed are ignored.

`main.go` wires the watcher through four hooks. All four are required for a
file-based type:

1. **`getManagedPaths(cfg)`** returns the absolute paths currently managed,
   based on the caches (or files that exist on disk). It is used by the next
   two hooks.
2. **`suppressManagedWrites(cfg, extraPaths...)`** must be called at the top
   of every `syncAll*` function, with every path the sync is about to write
   or delete. It suppresses watcher events for those paths and all managed
   paths for 2 seconds, so the agent's own writes do not trigger a restore
   loop.
3. **`defer updateWatcher(cfg)`** right after the suppression call. It
   refreshes the watched set after the sync, so new files are watched and
   removed files are dropped.
4. **`onTamperedFile`** must have a `case` that recognizes the type's paths
   (for example `strings.HasPrefix(path, policy.PolkitRulesDir+"/")`) and
   calls the type's `syncAll*` (or `trigger*` for long-running types) to
   restore the file. After the switch, the agent reports the event with
   `ReportTamperEvent`, including the processes that held the file open
   (`procinfo.FindFileHolders`).

The reference pattern is `syncAllPolkit`:

```go
func syncAllWidget(ctx context.Context, client *policyclient.Client, cfg *config.Config) {
    // 1. Compute desired paths and discover stale existing paths.
    desiredPaths := ...
    existingFiles, _ := policy.ListBorManagedWidgetFiles()

    // 2. Suppress watcher for all involved paths.
    suppressManagedWrites(cfg, append(desiredPaths, existingFiles...)...)
    defer updateWatcher(cfg)

    // 3. Remove stale files not in desiredPaths.
    // 4. Write new / updated files.
    // 5. Check compliance and report.
}
```

Currently watched locations: Firefox, Thunderbird, Chrome and Edge policy
JSON; KConfig files under `/etc/bor/xdg`, the two `XDG_CONFIG_DIRS` hook
scripts and, when enabled, `/etc/kde5rc`;
`/etc/dconf/`; `/etc/polkit-1/rules.d`; Bor package repository and key
files; firewalld zones; `/etc/security/time.conf`; the Flatpak state
directory `/etc/bor/flatpak`; the disk-encryption dracut drop-in and
`/etc/crypttab`. Flatpak's own `/var/lib/flatpak/repo/config` is
deliberately not watched.

!!! note "Concurrency"
    The watcher invokes the callback in a new goroutine, and package,
    Flatpak and disk-encryption syncs also run in goroutines. Only session
    access, packages and Flatpak guard their state with mutexes; the other
    caches are plain maps shared with the stream goroutine. Keep this in
    mind when adding code that reads caches outside the stream callback,
    and run tests with `go test -race`.

## Atomic file writes

Write managed files with `policy.WriteFileAtomically(path, data)`
(`agent/internal/policy/firefox.go`): it creates the parent directory,
writes a temporary file `.bor-tmp-*` in the same directory, sets mode 0644
and renames it over the target. Readers never see a half-written file, and
the rename is what the file watcher expects. For files that must not be
world readable, follow the same pattern with a stricter mode.

Server-supplied values that end up in file names are sanitized (for example
`sanitizeAlphanumeric` for polkit file names) so they can never introduce a
path separator.

## Privileged operations and external tools

The agent runs as root because it writes system configuration. It calls
these external programs and services; each is probed so that a missing
tool turns into an `INAPPLICABLE` or `ERROR` result instead of a crash:

| Tool | Used by |
|---|---|
| `dconf update` | dconf |
| `gsettings` | dconf compliance checks |
| `pkaction` | polkit action discovery |
| `firewall-cmd` | Firewalld (`--state`, `--get-default-zone`, `--check-config`, `--reload`) |
| `flatpak` (path from `flatpak.binary`) | Flatpak remotes and apps |
| PackageKit over the system D-Bus | package install / remove |
| `loginctl`, logind D-Bus | session access enforcement |
| `pam-auth-update` or `authselect`, `getent` | session access PAM integration, user and group resolution |
| `cryptsetup`, `systemd-cryptenroll`, `clevis` | disk encryption |
| `dracut`, `update-initramfs`, `mkinitcpio` | initramfs rebuilds after disk-encryption changes |
| `hostname --fqdn`, `plasmashell --version`, `gnome-shell --version` | heartbeat metadata |

Commands are run with a fixed binary name and constant or validated
arguments (`exec.Command(name, args...)`, never through a shell). Each
call site documents this with its `gosec` exception. Keep that property
when adding new calls.

## The `luks` subcommand

`bor-agent luks <command>` (`agent/cmd/agent/luks_cmd.go`) runs before the
daemon path and exits:

| Command | Purpose |
|---|---|
| `status` | Print the LUKS volumes, keyslots and platform facts (TPM2, Secure Boot, initramfs tool, `cryptsetup` and `clevis` versions) |
| `adopt [--device DEV]` | Ask for an existing passphrase once so Bor can escrow a recovery key for volumes it has no other credential for; `--device` limits it to one device or LUKS UUID |

`adopt` uses the enrolled mTLS credentials to talk to the policy port.
