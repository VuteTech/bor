# KDE Plasma (Kconfig)

The **KDE Plasma** policy type (technical name *Kconfig*) manages KDE Plasma
5 and 6 desktops through KDE's Kiosk framework. Bor writes system-wide KDE
configuration files, optionally marked immutable with KDE's `[$i]` marker,
so users cannot override them in their own `~/.config` files. This page
explains the settings, the files on the endpoint, the editor, merging and
how to check the result on a Plasma desktop.

## What it controls

| Group (in the editor) | Settings | KDE file and group written |
|---|---|---|
| Session & Shell | **Restrict shell access**, **Restrict KRunner command execution**, **Restrict logout, shutdown and restart**, **Lock desktop layout**, **Restrict configuring widgets while locked (Plasma 5)** | `kdeglobals`, `[KDE Action Restrictions]` keys `shell_access`; `run_command` and `action/run_command`; `logout` and `action/logout`; `plasma/plasmashell/unlockedDesktop`; `plasma/allow_configure_when_locked` |
| Application Menu Actions | **Restrict File › New**, **Restrict File › Open**, **Restrict File › Save** | `kdeglobals`, `[KDE Action Restrictions]` keys `action/file_new`, `action/file_open`, `action/file_save` |
| System Settings Restrictions | **System Settings Modules** | `kdeglobals`, `[KDE Control Module Restrictions]` |
| Window Manager | **Borderless maximized windows** | `kwinrc`, `[Windows]` |
| Screen Lock | **Lock automatically after inactivity**, **Inactivity timeout (minutes)**, **Grace period before a password is required (seconds)**, **Lock on resume from sleep**, **Lock the screen immediately at login**, **Lock screen wallpaper image path** | `kscreenlockerrc`, `[Daemon]` keys `Autolock`, `Timeout`, `LockGrace`, `LockOnResume`, `LockOnStart`; `[Greeter]` `WallpaperPlugin` and `[Greeter][Wallpaper][org.kde.image][General]` `Image` |
| Appearance | **Icon theme**, **Wallpaper plugin**, **Wallpaper image path**, **Wallpaper fill mode**, **Wallpaper background color** | `kdeglobals` `[Icons]`; `plasma-org.kde.plasma.desktop-appletsrc` |
| Security | **URL Restrictions** | `kdeglobals`, `[KDE URL Restrictions]` |

Restriction settings follow KDE's convention: the key is written as `false`
to restrict and `true` to allow. The editor hides that detail and shows a
**Restricted / Allowed** switch (for **Lock desktop layout**: **Locked /
Unlocked**); a newly added restriction starts restricted. Every setting
shows the exact file, group and key it writes.

!!! note "Screen lock units"
    **Inactivity timeout** is written as `Timeout=` in `kscreenlockerrc`
    and KDE interprets it in **minutes** (KDE's default is 5). **Grace
    period** is `LockGrace=` in **seconds**: after the screen locks, the
    session can be unlocked without a password for that long; `0` asks for
    the password immediately.

!!! warning "Desktop wallpaper image"
    **Wallpaper image path** writes the first containment of the Plasma
    desktop layout file, which does not match every desktop, so it does
    not take effect reliably. **Lock screen wallpaper image path** does
    work from system configuration. A reliable desktop wallpaper is part
    of the planned desktop layout feature.

!!! note "Legacy settings with no effect"
    Policies created with older Bor versions may still contain **Wallpaper
    Changes**, **Icon Changes**, **Autostart Changes**, **Color Scheme
    Changes** or **Cursor Theme Changes**. They wrote KDE's
    `[KDE Resource Restrictions]` group, which no Plasma 5 or 6 component
    reads. The agent ignores them, the editor lists them under **Legacy
    (no effect)** with a **Remove from policy** button, and a policy that
    contains nothing else is rejected when saved. To stop wallpaper or
    theme changes, enforce the Appearance settings or use **Lock desktop
    layout**; to control autostart, use **Restrict shell access** (KDE
    disables the user autostart folder with it) or hide the Autostart
    module.

## Files on the endpoint

| Path | Content | Config key |
|---|---|---|
| `/etc/bor/xdg/<file>` (for example `kdeglobals`, `kwinrc`, `kscreenlockerrc`) | System-wide KDE settings from all KDE Plasma policies, including System Settings module restrictions in `kdeglobals` | `kconfig.config_path` (default `/etc/bor/xdg`) |
| `/etc/xdg/plasma-workspace/env/00-bor.sh` | Puts the Bor directory first in `XDG_CONFIG_DIRS` for every Plasma session (sourced by `startplasma`, independent of the display manager) | Follows `kconfig.config_path` |
| `/etc/profile.d/99-bor.sh` | Same, for login shells and display managers whose session scripts source `/etc/profile` | Follows `kconfig.config_path` |
| `/etc/kde5rc` | Optional mirror of the System Settings module restrictions for very old KDE Frameworks releases; Plasma 5 and 6 read them from `kdeglobals` | `kconfig.write_kde5rc` (default `false`) |

Both hook scripts contain the same POSIX shell snippet:

```bash
case ":${XDG_CONFIG_DIRS:-/etc/xdg}:" in
  *":/etc/bor/xdg:"*) ;;
  *) export XDG_CONFIG_DIRS="/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}" ;;
esac
```

KDE searches `XDG_CONFIG_DIRS` for system-wide configuration, so the files
in `/etc/bor/xdg` take precedence over distribution defaults in `/etc/xdg`.
The variable is set when the session starts, so **users must log out and
back in** once after the first KDE Plasma policy reaches a node.

Every file written by the agent starts with a header:

```text
# This file is managed by Bor. Do not edit manually.
# Changes will be overwritten by policy enforcement.
```

**Backups.** Before writing a file for the first time, the agent copies
any existing file to `<file>.bor-backup` (an empty backup means the file
did not exist). The backup files are also how the agent remembers which
files it manages. When a file is no longer needed, or the last KDE Plasma
policy is removed, the original is restored or the file is deleted. The
hook scripts stay in place. Agents upgraded from a version that wrote
`/etc/kde6rc` (a file KDE never reads) restore that file at the next sync.

## Create a KDE Plasma policy

![KDE Plasma policy editor: Session & Shell with shell access restricted and enforced (immutable)](../images/screenshots/policy-editor-kconfig.webp){ loading=lazy }

1. Go to **Policies** and click **Create a policy**, then pick **KDE
   Plasma** on the **Type** step.
2. Enter a **Name** and **Description** on the **Details** step.
3. On the **Configuration** step pick a setting from the tree on the left.
   The right side shows the file, group and key the setting writes, a
   short explanation, and:
    - **Value**: a **Restricted / Allowed** switch for Kiosk restrictions,
      an **Enabled / Disabled** switch for other booleans, a text or
      number field, a drop-down for the fill mode, or a color picker
      (stored as `R,G,B`).
    - **Enforced (Immutable)**: when on (**Enforced [$i]**), the entry is
      written with KDE's immutability marker and users cannot override
      it. When off (**Not enforced**), the value is only a system default
      that users may change. New settings start as enforced.
    - **Add to policy** / **Remove from policy**.
4. **System Settings Modules** has its own editor: pick modules from
   **Add module** (each entry shows its description and module ID, for
   example `Autostart Applications (kcm_autostart)`), or open **Add custom
   modules** and enter module IDs separated by commas or new lines, then
   click **Add**.
5. **URL Restrictions** has a rule editor: click **+ Add Rule** and set
   **Action** (`open`, `list` or `redirect`), **Protocol**, **Host**,
   **Path** and **Access** (**Allow** or **Deny**). Blank fields match
   everything; `$HOME` and `$TMP` are expanded in paths. **Referrer
   Matching (advanced)** restricts the rule to requests coming from a given
   protocol, host or path.
6. Review, create and release the policy, then bind it to a node group.

URL restrictions and System Settings module restrictions are always
written as enforced.

## Examples

### Disable terminal access and KRunner commands

| Setting | Value | Enforced |
|---|---|---|
| **Restrict shell access** | Restricted | Enforced [$i] |
| **Restrict KRunner command execution** | Restricted | Enforced [$i] |

`/etc/bor/xdg/kdeglobals` then contains:

```text
[KDE Action Restrictions][$i]
action/run_command=false
run_command=false
shell_access=false
```

When every entry in a group is enforced, the whole group is marked `[$i]`;
otherwise only the enforced keys are marked (`shell_access[$i]=false`).

### Lock the desktop layout

| Setting | Value | Enforced |
|---|---|---|
| **Lock desktop layout** | Locked | Enforced [$i] |

```text
[KDE Action Restrictions][$i]
plasma/plasmashell/unlockedDesktop=false
```

Users can no longer move or add panels and widgets or edit launcher
favourites.

### Lock the screen after ten minutes

| Setting | Value | Enforced |
|---|---|---|
| **Lock automatically after inactivity** | Enabled | Enforced [$i] |
| **Inactivity timeout (minutes)** | `10` | Enforced [$i] |
| **Grace period before a password is required (seconds)** | `0` | Enforced [$i] |
| **Lock on resume from sleep** | Enabled | Enforced [$i] |

`/etc/bor/xdg/kscreenlockerrc`:

```text
[Daemon][$i]
Autolock=true
LockGrace=0
LockOnResume=true
Timeout=10
```

### Hide System Settings modules

**System Settings Modules**: add `kcm_autostart` (Autostart Applications),
`kcm_kded` (Background Services) and `kcm_krdpserver` (Remote Desktop).

`/etc/bor/xdg/kdeglobals` then also contains:

```text
[KDE Control Module Restrictions][$i]
kcm_autostart=false
kcm_kded=false
kcm_krdpserver=false
```

The modules disappear from System Settings and cannot be opened with
`kcmshell6` (or `kcmshell5`).

### Deny browsing Windows shares

**URL Restrictions**, **+ Add Rule**: **Action** `list`, **Protocol**
`smb`, **Host** and **Path** blank, **Access** **Deny**. Bor writes:

```text
[KDE URL Restrictions][$i]
rule_1=list,,,,smb,,,false
rule_count=1
```

## How multiple KDE Plasma policies are merged

All KDE Plasma policies that apply to a node are written into the same set
of files:

- Each key is written once. When two policies set the same key, the policy
  with the **higher** binding priority wins; with equal priority the
  policy with the lexically smaller ID wins, so the result does not depend
  on the order in which changes arrive.
- The losing policy reports that setting as **Inapplicable** with the
  message "Overridden by higher-priority policy *name*; applied value:
  ..." on the compliance page. If both policies set the same value, both
  report it as compliant.
- URL restriction rules from all policies are collected and renumbered
  (`rule_1`, `rule_2`, ...), with one `rule_count`.
- System Settings module lists are combined.

## What users see

- Restricted actions disappear or are disabled (for example the terminal
  shortcut or KRunner command execution), and restricted System Settings
  modules are hidden.
- Enforced settings are locked: the corresponding controls in System
  Settings are disabled or missing, and edits to the user's own config
  files have no effect.
- After a change, the agent asks KWin, the Plasma shell and the screen
  locker to reload their configuration, and can show a desktop
  notification (default text: "Desktop policies have been updated. Please
  log out and log back in for all changes to take effect."). Configure it
  under **Settings -> Agent Notifications**.

Some settings are only read when Plasma starts; log out and back in to be
sure everything applies.

## Compliance

Every setting is reported as its own item on the compliance page:
**Compliant** once it is written to the managed file, **Inapplicable** when
a higher-priority policy overrides it, and **Error** when a value could not
be written (for example a value containing a line break). A policy that
only contains legacy settings is reported as **Inapplicable** with an
explanation.

## Verify on the node

```bash
# Files written by the agent and their backups
ls -la /etc/bor/xdg/
sudo cat /etc/bor/xdg/kdeglobals
cat /etc/xdg/plasma-workspace/env/00-bor.sh

# The search path a Plasma session gets (run inside the session)
echo $XDG_CONFIG_DIRS

# Read values as KDE sees them (run as the desktop user, inside the session)
kreadconfig6 --file kdeglobals --group "KDE Action Restrictions" --key shell_access
kreadconfig6 --file kdeglobals --group "KDE Control Module Restrictions" --key kcm_autostart
kreadconfig6 --file kscreenlockerrc --group Daemon --key Autolock
```

On Plasma 5 use `kreadconfig5`.

## Troubleshooting

**Nothing changes on the desktop.**

- Make sure the user logged in again after the first policy arrived, so
  that the hook scripts set `XDG_CONFIG_DIRS`. Check with
  `echo $XDG_CONFIG_DIRS` in a terminal of the session (if one is
  allowed), or `cat /proc/<pid-of-plasmashell>/environ | tr '\0' '\n'`.
- Compliance for this type confirms the files were written and which
  policy won each key; it does not read values back from the session yet.

**A setting is shown but users can still change it.** Turn on **Enforced
(Immutable)** for that setting. Non-enforced values are defaults only.

**System Settings still shows a restricted module.** Check that the module
ID is correct (it must match the module's ID, for example `kcm_autostart`)
and that `/etc/bor/xdg/kdeglobals` contains it. Restart System Settings.
On a very old KDE Frameworks release that does not read restrictions from
`kdeglobals`, set `kconfig.write_kde5rc: true` in the agent configuration
so the agent also mirrors them into `/etc/kde5rc`.

**A policy reports Inapplicable.** Either every setting in it is
overridden by a higher-priority policy (the item messages name it), or the
policy only contains legacy settings; open it and remove them.

**Files were changed back after editing them.** Files under `/etc/bor/xdg`,
the hook scripts and `/etc/kde5rc` (when mirrored) are tamper-protected.
The agent restores them and records a `tamper_detected` event in the
[audit log](../user-guide/audit-log.md).
