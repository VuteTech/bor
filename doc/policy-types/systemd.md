# Services & timers (systemd)

The **Services & timers** policy type (technical name `Systemd`) manages
systemd units on your nodes: whether a service, timer, socket, target or
path unit is enabled, disabled or masked, whether it is running or stopped,
and overrides for a timer's schedule or a service's restart, resource and
sandboxing settings. This page explains what the agent changes on the
endpoint, how to build a policy in the editor, how several policies are
merged, and how to verify and troubleshoot the result.

!!! note "Existing units only"
    This policy type manages units that are already installed on the node.
    It does not deploy custom units or scripts, does not write systemd
    preset files, and does not manage per-user units
    (`systemctl --user`).

## What it controls

For every unit listed in a bound policy, the agent talks to systemd over
the D-Bus system bus (`org.freedesktop.systemd1`), the same interface
`systemctl` uses, and brings three things in line with the policy:

- **Enablement** - the unit-file state: **Enabled**, **Disabled** or
  **Masked**. Enabling and disabling create or remove the symlinks the
  unit's `[Install]` section describes; masking links the unit to
  `/dev/null` so it cannot start at all, not even as a dependency of
  another unit.
- **Activity** - whether the unit is **Running** or **Stopped** right now.
  The agent re-asserts this on every sync and on every reconcile pass, so a
  service that is stopped by hand comes back.
- **Overrides** - a drop-in file
  `/etc/systemd/system/<unit>.d/90-bor.conf` with `[Timer]` settings for a
  timer (schedule, randomized delay, persistence) or `[Service]` settings
  for a service (restart policy, timeouts, environment, memory and CPU
  limits, sandboxing). The vendor unit file is never edited.

Each of the three is optional. A unit entry that sets only an enablement
leaves the activity alone, and the other way round. An entry can also
require that the **last run** of a timer or service succeeded.

Typical uses: the systemd half of a hardening baseline (mask `avahi-daemon`
and `cups-browsed`, disable Bluetooth, mask `ctrl-alt-del.target`), moving a
distribution's maintenance timers into a maintenance window with a
randomized delay so a fleet does not wake up at once, and keeping an agent
or monitoring service running with a restart policy and a memory limit.

Every drop-in Bor writes starts with the line
`# Managed by Bor - changes are overwritten`.

## Endpoint prerequisites

- **systemd as the init system.** The agent checks for
  `/run/systemd/system`; without it (a container, or a distribution that
  does not boot with systemd) every policy of this type is reported as
  **inapplicable** with the message
  `systemd is not the init system on this node`.
- **The D-Bus system bus.** The agent runs as root and needs to reach
  `org.freedesktop.systemd1`. If the bus cannot be reached the policies are
  reported as **error** (`cannot reach systemd on the system bus`).
- **`systemd-analyze`** (part of the systemd package on every supported
  distribution). The agent uses it to check calendar expressions before it
  writes a timer drop-in. Without it the server-side syntax check is the
  only check.
- systemd **249 or newer** (Ubuntu 22.04, SLES 15 SP4, Debian 12, RHEL 9
  and current Fedora all qualify). The timer settings **FixedRandomDelay**,
  **OnClockChange** and **OnTimezoneChange** exist in the schema and need
  systemd 247 and 242 respectively; the editor does not offer them.

No extra packages are needed.

## Agent configuration

The agent reads these keys from `/etc/bor/config.yaml`. All are optional.

| Key | Default | Description |
|---|---|---|
| `systemd.system_dir` | `/etc/systemd/system` | Directory where the agent writes the `<unit>.d/90-bor.conf` drop-ins. |
| `systemd.state_path` | `/var/lib/bor/agent/systemd-state.json` | Record of each managed unit's original state, used to restore it when the unit leaves every policy. |
| `systemd.job_timeout` | `90s` | How long the agent waits for a start, stop or restart job to finish. |
| `systemd.reconcile_interval` | `10m` | How often the agent re-checks and re-asserts the enablement and activity of every managed unit while a policy applies. `0` disables the periodic pass. |
| `systemd.analyze_binary` | `systemd-analyze` | The executable used to validate calendar expressions, resolved through `PATH` unless absolute. |

```yaml
systemd:
  reconcile_interval: 5m
```

## Create a services & timers policy

1. Open **Policies** and click **Create a policy**.
2. Choose the **Services & timers** tile and enter a name and description.
3. Fill in the editor (see below) and save the policy.
4. Click **Release**. Only released policies are delivered to agents.
5. Bind the policy to one or more node groups on the
   [bindings](../user-guide/bindings.md) page.

See [Policies](../user-guide/policies.md) for the general policy lifecycle.

The editor rejects an empty policy: add at least one unit, and give each
unit at least one setting (an enablement, an activity, an override or the
last-run check).

!!! tip "Pilot group first"
    Stopping a display manager or masking the network stack on a whole
    node group is one release away. Bind a new policy to a small pilot
    group first, check the [compliance page](../user-guide/compliance.md),
    then widen the binding.

### Editor sections

**Units**

The editor shows one card per unit. **Add unit** appends a card; the trash
button removes one.

- **Show the state on node** - pick a node to see, under every unit name,
  what that node reports right now (**Now on lab-01: enabled, active
  (running)**), its schedule for timers and the unit's description. Nodes
  report their installed units to the server when the agent connects and
  after every sync that changed something, so the states are a few seconds
  old at most. With **Units reported by any node** the picker lists the
  union of all nodes' units instead.
- **Unit** - the full unit name with its suffix: `bluetooth.service`,
  `fstrim.timer`, `cups.socket`, `ctrl-alt-del.target`, `cups.path`.
  Typing filters the units nodes have reported (each suggestion shows the
  type and how many nodes reported it); a name no node has reported is
  accepted as typed, so you can write a policy before the package is
  installed. Names may contain letters, digits, `:`, `_`, `.`, `\`, `@`
  and `-`, must end in `.service`, `.timer`, `.socket`, `.target` or
  `.path`, and are at most 255 characters long. Template units such as
  `getty@.service` are refused; name an instance (`getty@tty2.service`)
  instead.
- **Enablement** - **Keep**, **Enabled**, **Disabled** or **Masked**.
  **Keep** leaves the unit-file state as it is.
- **Activity** - **Keep**, **Running** or **Stopped**. Hidden for targets,
  which are only enabled, disabled or masked. A masked unit cannot be
  running; the editor refuses the combination.
- **Restart after override changes** (services only) - restart a running
  service when its drop-in changes, so new settings take effect at once.
  Off by default, because restarting a service such as `cups.service`
  interrupts its users. Timers are always restarted after a schedule
  change; that only reschedules them.
- **Last run must have succeeded** (timers and services) - the unit's last
  run must have ended with `success` for the unit to be compliant. For a
  timer the agent looks at the service the timer activates.
- **Description** - free text shown in the policy summary.

When a unit is on the risky list (network, SSH, display managers, the
firewall, time synchronization, audit, cron; see
[Security considerations](#security-considerations)), the card shows a
warning (**<unit> can cut users or administrators off from the node**) and
the policy cannot be saved until you tick **I understand; manage <unit>
anyway**. Units on the protected list are refused outright.

If the chosen node reports a unit as `static` (no `[Install]` section), the
editor reminds you that it cannot be enabled or disabled and that you
should set an activity instead.

**Overrides**

An expandable section under each unit. For a `.timer` unit it offers the
timer settings, for a `.service` unit the service settings; other types
have no overrides. Each field maps to one systemd directive and takes the
same value syntax as the unit file would.

Timer settings:

| Field | Directive | Value |
|---|---|---|
| **Schedule preset** | | Fills **OnCalendar** with one of: **Hourly** (`hourly`), **Daily at 03:00** (`*-*-* 03:00:00`), **Weekdays at 07:00** (`Mon..Fri *-*-* 07:00:00`), **Weekly, Saturday 03:00** (`Sat *-*-* 03:00:00`), **Monthly, first day 03:00** (`*-*-01 03:00:00`) or **Custom expression**. |
| **OnCalendar** | `OnCalendar=` | One or more calendar expressions (`systemd.time(7)`), at most 16. They replace the vendor schedule. |
| **OnBootSec** | `OnBootSec=` | Time span after boot. |
| **OnUnitActiveSec** | `OnUnitActiveSec=` | Time span after the last activation (a repeating interval). |
| **RandomizedDelaySec** | `RandomizedDelaySec=` | Random delay added to every trigger, to spread a fleet over a window. |
| **AccuracySec** | `AccuracySec=` | How much the trigger may be delayed to coalesce wake-ups (systemd's default is 1 minute). |
| **Persistent** | `Persistent=` | **Yes** catches up a run that was missed while the node was off; essential for laptops. |
| **WakeSystem** | `WakeSystem=` | **Yes** wakes a suspended node for the run. |

Service settings:

| Field | Directive | Value |
|---|---|---|
| **Restart** | `Restart=` | `no`, `on-success`, `on-failure`, `on-abnormal`, `on-abort`, `on-watchdog` or `always`. |
| **RestartSec** | `RestartSec=` | Time span. |
| **TimeoutStartSec**, **TimeoutStopSec** | `TimeoutStartSec=`, `TimeoutStopSec=` | Time spans. |
| **Environment** | `Environment=` | `KEY=value` entries, at most 64, added to the vendor environment. Values are stored in the policy and visible to everyone who can read it; keep secrets out. |
| **MemoryMax** | `MemoryMax=` | A size such as `512M`, a percentage of physical memory such as `20%`, or `infinity`. |
| **CPUQuota** | `CPUQuota=` | A percentage such as `50%` (more than `100%` uses several cores). |
| **TasksMax** | `TasksMax=` | A number, a percentage or `infinity`. |
| **Nice** | `Nice=` | An integer from -20 to 19. |
| **NoNewPrivileges** | `NoNewPrivileges=` | **Yes** or **No**. |
| **ProtectSystem** | `ProtectSystem=` | `no`, `yes` (read-only `/usr` and `/boot`), `full` (also `/etc`) or `strict` (whole file system). |
| **ProtectHome** | `ProtectHome=` | `no`, `yes` (inaccessible), `read-only` or `tmpfs`. |
| **PrivateTmp** | `PrivateTmp=` | **Yes** or **No**. |

**Keep** (or an empty field) leaves the directive untouched.

Time spans use systemd syntax: `90s`, `15min`, `1h 30min`, `1.5h`, a bare
number of seconds, or `infinity`. Calendar expressions use systemd syntax:
`daily`, `Sat *-*-* 03:00:00`, `Mon..Fri 07:00`, `*-*-01 00:00:00`. The
server checks the syntax of both when you save (and runs
`systemd-analyze calendar` when it runs on a systemd host); the agent runs
`systemd-analyze calendar` on the node before it writes the drop-in.

## Setting reference

Policy content is stored as JSON (the `SystemdPolicy` message). Field names
use camelCase; enumerations are stored by name.

| Field | Type | Description |
|---|---|---|
| `units` | list of unit entries | At least one, at most 200. A unit name appears once per policy. |

Unit entry:

| Field | Type | Description |
|---|---|---|
| `name` | string | Full unit name with suffix. |
| `enablement` | string | `SYSTEMD_ENABLEMENT_ENABLED`, `SYSTEMD_ENABLEMENT_DISABLED` or `SYSTEMD_ENABLEMENT_MASKED`. Omitted = keep. |
| `activity` | string | `SYSTEMD_ACTIVITY_RUNNING` or `SYSTEMD_ACTIVITY_STOPPED`. Omitted = keep. Not allowed for targets; not allowed with `MASKED` and `RUNNING` together. |
| `restartOnChange` | boolean | Services only. Restart a running service after its drop-in changed. |
| `requireLastRunSuccess` | boolean | Timers and services only. The last run must have succeeded. |
| `description` | string | Free text, at most 512 characters. |
| `dropIn.timer` | object | Timer settings (`.timer` units only): `onCalendar` (list), `onBootSec`, `onStartupSec`, `onActiveSec`, `onUnitActiveSec`, `onUnitInactiveSec`, `persistent`, `randomizedDelaySec`, `accuracySec`, `wakeSystem`, `fixedRandomDelay`, `onClockChange`, `onTimezoneChange`. |
| `dropIn.service` | object | Service settings (`.service` units only): `restart`, `restartSec`, `timeoutStartSec`, `timeoutStopSec`, `environment` (list), `memoryMax`, `cpuQuota`, `tasksMax`, `nice`, `noNewPrivileges`, `protectSystem`, `protectHome`, `privateTmp`. |

What **Running** means depends on the unit:

- For a service with a long-running main process, for a timer, a socket or
  a path: the unit is `active` (or on its way to it: `activating`,
  `reloading`).
- For a `Type=oneshot` service without `RemainAfterExit=`, which is never
  `active` after it ran: the service ran successfully since boot (its
  result is `success` and it has a start timestamp). The agent starts it
  when that is not the case. Prefer managing the timer that triggers such
  a service.

**Stopped** means the unit is not active (`inactive`, `failed` or
`deactivating`).

### Example: desktop hardening baseline

Mask the services a managed desktop does not need, disable Bluetooth, and
make Ctrl+Alt+Del do nothing:

```json
{
  "units": [
    { "name": "avahi-daemon.service", "enablement": "SYSTEMD_ENABLEMENT_MASKED" },
    { "name": "avahi-daemon.socket", "enablement": "SYSTEMD_ENABLEMENT_MASKED" },
    { "name": "cups-browsed.service", "enablement": "SYSTEMD_ENABLEMENT_MASKED" },
    {
      "name": "bluetooth.service",
      "enablement": "SYSTEMD_ENABLEMENT_DISABLED",
      "activity": "SYSTEMD_ACTIVITY_STOPPED"
    },
    { "name": "ctrl-alt-del.target", "enablement": "SYSTEMD_ENABLEMENT_MASKED" }
  ]
}
```

Masking implies stopping: a masked unit that is active is stopped before
the mask is applied.

### Example: maintenance window

Keep the weekly TRIM job enabled and waiting, move it to Saturday 03:00 with
up to one hour of random delay so the fleet does not run it at once, catch
up a run missed while a laptop was off, and flag a node whose last run
failed:

```json
{
  "units": [
    {
      "name": "fstrim.timer",
      "enablement": "SYSTEMD_ENABLEMENT_ENABLED",
      "activity": "SYSTEMD_ACTIVITY_RUNNING",
      "requireLastRunSuccess": true,
      "dropIn": {
        "timer": {
          "onCalendar": ["Sat *-*-* 03:00:00"],
          "persistent": true,
          "randomizedDelaySec": "1h"
        }
      }
    }
  ]
}
```

The agent writes `/etc/systemd/system/fstrim.timer.d/90-bor.conf`:

```ini
# Managed by Bor - changes are overwritten
# Unit: fstrim.timer

[Timer]
OnCalendar=
OnCalendar=Sat *-*-* 03:00:00
Persistent=yes
RandomizedDelaySec=1h
```

The empty `OnCalendar=` line resets the vendor schedule, so the Bor schedule
replaces it instead of adding a second trigger. The agent restarts the timer
after the reload so the new schedule applies.

### Example: service hardening

Keep a monitoring agent running, restart it when it fails, cap its memory
and sandbox it:

```json
{
  "units": [
    {
      "name": "node-exporter.service",
      "enablement": "SYSTEMD_ENABLEMENT_ENABLED",
      "activity": "SYSTEMD_ACTIVITY_RUNNING",
      "restartOnChange": true,
      "dropIn": {
        "service": {
          "restart": "on-failure",
          "restartSec": "5s",
          "memoryMax": "256M",
          "noNewPrivileges": true,
          "protectSystem": "strict",
          "protectHome": "yes",
          "privateTmp": true
        }
      }
    }
  ]
}
```

!!! warning "Sandboxing can break a service"
    `ProtectSystem=strict` and `ProtectHome=yes` make most of the file
    system read-only or invisible to the service. A service that writes
    outside `/var` or `/run` fails to start under these settings. The
    agent reports such a unit as **error** with systemd's job result; the
    drop-in stays, so fix the policy rather than the node.

## Merge behavior

When several services & timers policies apply to the same node, the agent
merges them into one desired state per unit. Each part of a unit entry is
merged on its own:

- **Enablement** and **activity**: the policy with the **higher** binding
  priority wins. On equal priority the more restrictive value wins:
  masked over disabled over enabled, stopped over running. A unit that
  ends up **masked** is always **stopped**, whatever another policy says
  about its activity.
- **Timer settings** are one block: the highest-priority policy that sets
  any timer setting provides the whole block; other policies' timer
  settings for that unit are ignored.
- **Service settings** are merged **directive by directive**: for each
  directive the highest-priority policy that sets it wins. **Environment**
  entries are a union keyed by variable name, the higher priority winning
  a duplicate.
- **Restart after override changes** and **Last run must have succeeded**
  are on if any policy turns them on.

Every policy reports its own result for every unit it lists. A policy whose
value for a unit lost reports that unit as **inapplicable** with the
message `overridden: enablement by <policy> (priority N)` (or `activity`,
`timer settings`, `service settings <directive>`), followed by the unit's
current state. Two policies that ask for the same value are both compliant.

## What the agent changes on the node

Files, written atomically and recognized by their first line:

| Path | Content |
|---|---|
| `/etc/systemd/system/<unit>.d/90-bor.conf` | The unit's overrides, one file per unit, only when the unit has timer or service settings. The `90-` prefix sorts after the usual distribution drop-ins (`10-`, `50-`), so Bor's values win. |
| `/var/lib/bor/agent/systemd-state.json` | The unit-file state and activity each managed unit had the first time Bor touched it (`unit_file_state`, `active`, `recorded_at`), used for the restore described below. Removed when no unit is recorded. |

Through systemd (nothing else on disk is touched by Bor itself):

- `EnableUnitFiles` and `DisableUnitFiles` create and remove the
  `[Install]` symlinks under `/etc/systemd/system/`; `MaskUnitFiles` and
  `UnmaskUnitFiles` create and remove the `/dev/null` link. The agent never
  uses systemd's force option, so a unit whose own file lives in
  `/etc/systemd/system/` is not replaced by a mask (see
  [Troubleshooting](#troubleshooting)).
- `StartUnit`, `StopUnit` and `RestartUnit` queue a job in `replace` mode
  and the agent waits for its completion (`systemd.job_timeout`), so a
  failed start is reported with systemd's job result.
- A daemon reload (`Reload`) after any drop-in or enablement change. After
  the reload the agent reads every unit whose drop-in changed back; a unit
  that systemd now reports as `bad-setting` or `error` has its drop-in
  removed again, systemd is reloaded once more and the unit is reported
  as **error** (`systemd rejected the drop-in (load state bad-setting); it
  was removed`).

A sync runs these steps in order: drop-ins, reload, restore of units that
left every policy, enablement (with one reload), activity, state file,
then one more read of every unit for the compliance report. Syncs run in
the background, one at a time, so a slow stop never blocks the policy
stream.

## Drift handling

Three mechanisms keep a node in line between policy changes:

- **File watcher.** Every `90-bor.conf` the agent wrote and the state
  file are watched. Editing or deleting one re-runs the sync, which
  rewrites the file, reloads systemd and records a `tamper_detected` event
  in the [audit log](../user-guide/audit-log.md).
- **Reconcile pass.** Every `systemd.reconcile_interval` (default 10
  minutes) while at least one policy applies, the agent re-reads every
  managed unit and re-asserts its enablement and activity: a service
  stopped by hand is started again, a unit re-enabled by hand is disabled
  again, and the compliance report is refreshed. The action is noted in
  the compliance message (`; started`, `; disabled`).
- **Catalogue refresh.** After a sync that changed a file or a unit state,
  the agent reports its unit inventory to the server again, so the editor's
  **Show the state on node** view stays current.

Manual changes to a unit's state are not reported as tamper events; they
show up as the reconcile pass undoes them.

## Removal and restore

The first time a sync touches a unit, the agent records the unit's
unit-file state and whether it was active in the state file. When the unit
leaves every policy (the policy was unbound, deleted, unpublished, or the
unit was removed from it), the next sync:

1. removes the unit's `90-bor.conf` and reloads systemd;
2. restores the recorded enablement: unmasks a unit Bor masked, masks a
   unit that was masked before, enables or disables it as it was;
3. restores the recorded activity: starts a service Bor stopped if it was
   running before, stops one Bor started if it was not;
4. drops the record. The state file is deleted when it is empty.

!!! warning "Restore brings services back"
    Restore is deliberate: a unit returns to what it was before Bor touched
    it. If Bluetooth was enabled and running on a node when the baseline
    arrived, unbinding the baseline enables and starts it again. To keep a
    hardening decision after a policy is retired, replace the policy
    rather than unbinding it.

When no services & timers policy applies to the node any more, every
drop-in is removed and every recorded unit is restored this way.

## Compliance

The agent reports one item per unit, under the policy, on the
[compliance page](../user-guide/compliance.md). The item's message always
ends with the unit's current state in the form
`enabled, active (running)`; for timers it adds the next and last run,
`next Sat 2026-10-24 03:00, last Sat 2026-10-17 03:12 (success)`.

| Item state | When |
|---|---|
| Compliant | The parts of the entry that won the merge match the live state and the drop-in on disk matches the policy. The message may add what the sync did (`; stopped`, `; masked`, `; unmasked`, `; enabled`, `; restarted`). |
| Non-compliant | A state differs (`expected masked, unit file is enabled`, `expected running, unit is inactive (dead)`), the drop-in is missing or differs from the policy, systemd has not reloaded it yet, or the last run failed (`last run of fstrim.service failed (exit-code, exit status 1)`). |
| Inapplicable | The unit is not installed on the node (`unit is not installed on this node`), every part of the entry was overridden by a higher-priority policy, or a start was skipped because a condition of the unit is not met. |
| Error | The unit is protected, its name is invalid, systemd could not be queried, a job failed (`start failed (job result failed)`), the unit is `static`, `generated` or `transient` and the entry asks to enable or disable it, a mask was refused, or the drop-in was rejected. |

The policy's status is the roll-up of its items: any error gives
**Error**, otherwise any non-compliant item gives **Non-compliant**,
otherwise **Inapplicable** when no unit applies, otherwise **Compliant**
with the message `Deployed`.

## Verify on the endpoint

```bash
# Unit-file state and activity as systemd sees them
systemctl is-enabled bluetooth.service
systemctl is-active bluetooth.service
systemctl status fstrim.timer

# The vendor unit with Bor's drop-in appended
systemctl cat fstrim.timer

# Next and last run of every timer
systemctl list-timers --all

# What a timer's service did on its last run
journalctl -u fstrim.service -n 20

# Drop-ins Bor manages and the recorded original states
ls -l /etc/systemd/system/*.d/90-bor.conf
sudo cat /var/lib/bor/agent/systemd-state.json

# The agent's log
sudo journalctl -u bor-agent | grep -i systemd
```

The agent logs every start, stop, restart and restore it performs with the
job result (`systemd: stop bluetooth.service: done`,
`systemd: restored bluetooth.service to its original state (enabled,
active=true)`), and each sync's resulting compliance status.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Inapplicable: `systemd is not the init system on this node` | The node does not run systemd as PID 1 (a container, or another init). The policy type cannot apply there. |
| Error: `cannot reach systemd on the system bus` | The D-Bus system bus is down or the agent cannot connect to it. Check `systemctl status dbus` on the node and restart the agent. |
| Inapplicable: `unit is not installed on this node` | No unit file of that name exists on the node. Install the package, or remove the unit from the policy. Compare with the state another node reports in the editor. |
| Error: `unit is static and has no [Install] section; manage its activity instead` | Static units (and `generated` or `transient` ones) cannot be enabled or disabled. Set **Enablement** to **Keep** and use **Activity**. |
| Error: `mask: ... File /etc/systemd/system/<unit> already exists` | The unit's own file lives in `/etc/systemd/system/` (an administrator-written unit). Masking would replace that file, which the agent refuses. Move the unit file elsewhere or manage it differently. |
| Error: `systemd rejected the drop-in (load state bad-setting); it was removed` | systemd could not load the unit with the override, usually because a directive does not apply to the unit's type on this systemd version. The agent removed the drop-in and reloaded; fix the override. |
| Error: `start failed (job result failed)` or `(job result dependency)` | The unit could not start. Run `systemctl status <unit>` and `journalctl -u <unit>` on the node; a sandboxing override is a common cause. `dependency` means a unit it requires failed. |
| Error: `start failed (job result timeout)` | The job did not finish within `systemd.job_timeout`. Raise it for services with long start-up, or fix the service. |
| Inapplicable: `start skipped: a condition of the unit is not met` | The unit has a `Condition*=` or `Assert*=` that is false on this node, so systemd skipped it. Nothing to fix unless the condition is wrong. |
| Non-compliant: `systemd has not reloaded the drop-in yet` | Another process changed unit files without a daemon reload. The next sync or reconcile pass reloads; or run `sudo systemctl daemon-reload`. |
| Non-compliant: `last run of <service> failed (...)` | The timer's service failed on its last run. Read `journalctl -u <service>` on the node. |
| Saving fails with `invalid time span` or `invalid calendar expression` | Use systemd syntax (`15min`, `1h 30min`, `Sat *-*-* 03:00:00`). The message names the field. |
| Saving fails with `this unit is protected` | The unit is on the protected list below and cannot be managed by a policy. |

## Security considerations

- **Lockouts.** A policy that stops `gdm.service` or masks
  `NetworkManager.service` applies to every node of the group within
  seconds of its release. The editor asks for a confirmation for these
  **risky** units: `NetworkManager.service`, `systemd-networkd*`,
  `systemd-resolved.service`, `sshd.service` and `ssh.service` (and their
  sockets), `gdm.service`, `sddm.service`, `lightdm.service`,
  `display-manager.service`, `firewalld.service`, `chronyd.service`,
  `chrony.service`, `systemd-timesyncd.service`, `auditd.service`,
  `cron.service`, `crond.service`, `cronie.service`,
  `wpa_supplicant.service` and `iwd.service`. Use a pilot group.
- **Protected units** are refused by the server and ignored by the agent:
  `bor-agent.service`, D-Bus (`dbus.service`, `dbus.socket`,
  `dbus-broker.service`), `systemd-journald*`, `systemd-logind.service`,
  `systemd-udevd*`, `polkit.service`, `user@*.service`,
  `user-runtime-dir@*.service`, `systemd-user-sessions.service`,
  `init.scope`, `-.slice`, `-.mount` and the boot targets (`basic`,
  `sysinit`, `multi-user`, `graphical`, `default`, `getty`, `rescue`,
  `emergency`, `shutdown`, `final`, `umount`, `reboot`, `poweroff`,
  `halt`, `kexec`). `ctrl-alt-del.target` is allowed on purpose.
- **No code execution.** This policy type never deploys scripts or custom
  units and never passes policy content to a shell. Unit names are
  validated on the server and again on the agent before they become a path
  component or a D-Bus argument; drop-in values are validated field by
  field and written into the drop-in file, never into a command line. The
  only external command is `systemd-analyze` with a constant argument
  list.
- **Environment values** set through a service override are stored in the
  policy and visible to everyone who can read policies. Keep credentials
  out of them; point the service at a file provisioned another way
  instead.
- **Validation before activation.** Calendar expressions are checked with
  `systemd-analyze` before a drop-in is written, and a drop-in systemd
  cannot load is removed again after the reload, so a bad override does
  not leave a unit unloadable.
