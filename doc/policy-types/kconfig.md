# KDE Plasma (Kconfig)

The **KDE Plasma** policy type (technical name *Kconfig*) manages KDE Plasma
5 and 6 desktops and KDE applications through KDE's configuration system
and Kiosk framework. Bor writes system-wide KDE configuration files,
optionally marked immutable with KDE's `[$i]` marker, so users cannot
override them in their own `~/.config` files. This page explains the
settings catalogue, the files on the endpoint, the editor, merging and how
to check the result on a Plasma desktop.

## What it controls

The editor offers a catalogue of KDE settings grouped by purpose. Every
entry shows the file, group and key it writes, a short explanation and,
where one exists, the closest Windows Group Policy setting.

| Category | Examples | Files written |
|---|---|---|
| Session & Shell | Restrict shell access, KRunner commands, logout and shutdown, screen locking, user switching, password reveal button, text completion, Get New... downloads, untrusted `.desktop` files | `kdeglobals` `[KDE Action Restrictions]` |
| Plasma Desktop | Lock desktop layout (panels, widgets, favourites), scripting console, activities | `kdeglobals` `[KDE Action Restrictions]` |
| Application Menu Actions, File Dialog | The standard File, Edit, Settings and Help actions of every KDE application; file dialog actions | `kdeglobals` `[KDE Action Restrictions]` |
| File Manager (Dolphin), Konsole, KRunner, Okular, Ark, KWallet | Application behaviour: full path in the location bar, Open Terminal entry, previews, delete confirmations, Konsole profile, search history, wallet idle timeout, recent documents | `dolphinrc`, `kiorc`, `konsolerc`, `krunnerrc`, `okularpartrc`, `arkrc`, `kwalletrc` |
| Screen Lock | Lock after inactivity, timeout, grace period, password required, lock on resume, lock at login, lock screen wallpaper | `kscreenlockerrc` |
| Session | Session restore mode, logout confirmation, excluded applications | `ksmserverrc` |
| Power (Plasma 6) | Display off and dimming timeouts, idle action, lid and power button actions, power profile, battery levels; plugged in and on battery | `powerdevilrc` |
| Appearance, Window Manager | Global theme, colour scheme, icon and cursor theme, fonts, splash screen, single click, window decoration and focus policy | `kdeglobals`, `kcminputrc`, `plasmarc`, `ksplashrc`, `kwinrc` |
| Launcher Favourites | Favourites prepended or appended for new users | `kicker-extra-favoritesrc` |
| Input & Locale | Keyboard layouts and options, NumLock, key repeat, language and formats | `kxkbrc`, `kcminputrc`, `plasma-localerc` |
| Privacy & Data | Telemetry level, clipboard history, document history, file indexing, keystroke snooping by legacy X11 applications | `PlasmaUserFeedback`, `klipperrc`, `kactivitymanagerd-pluginsrc`, `baloofilerc`, `kwinrc` |
| Devices & Services | Automatic mounting of removable devices, RDP server autostart | `kded_device_automounterrc`, `krdpserverrc` |
| Network (KIO) | Proxy configuration for KDE applications | `kioslaverc` |
| System Settings & URL Rules | Hide System Settings modules, or show only the listed ones; KIO URL restrictions | `kdeglobals` `[KDE Control Module Restrictions]`, `[KDE URL Restrictions]` |
| Custom Entries | Any other KDE configuration key: file, group, key, value and markers | the file you name |
| Advanced | Lock the whole `kdeglobals` file, import an INI file, preview the rendered files | |

Restriction settings follow KDE's convention: the key is written as `false`
to restrict and `true` to allow. The editor hides that detail and shows a
**Restricted / Allowed** switch; a newly added restriction starts
restricted.

Settings marked **Plasma 6** (power management, Wayland options) are
written on every node but only Plasma 6 reads them. Plasma 5 nodes ignore
them.

!!! note "Screen lock units"
    **Inactivity timeout** is written as `Timeout=` in `kscreenlockerrc`
    and KDE interprets it in **minutes** (KDE's default is 5). **Grace
    period** is `LockGrace=` in **seconds**: after the screen locks, the
    session can be unlocked without a password for that long; `0` asks for
    the password immediately.

!!! warning "Desktop wallpaper"
    The catalogue has no desktop wallpaper setting on purpose: the Plasma
    layout file holds one wallpaper per desktop containment, and writing a
    fixed containment number does not take effect reliably. Use **Lock
    screen wallpaper image** (Screen Lock), which does work from system
    configuration, or a global theme package that carries your wallpaper.

!!! note "Policies created with older Bor versions"
    Older policies stored a fixed set of typed fields. They keep working on
    the nodes unchanged. When such a policy is opened in the editor, the
    fields are converted to catalogue settings, and the former **Resource
    Restrictions** (wallpaper, icon, autostart, colour and cursor changes),
    which KDE Plasma 5 and 6 never read, are removed with a notice.

## Files on the endpoint

| Path | Content | Config key |
|---|---|---|
| `/etc/bor/xdg/<file>` (for example `kdeglobals`, `kwinrc`, `kscreenlockerrc`, `dolphinrc`) | System-wide KDE settings from all KDE Plasma policies, including System Settings module restrictions in `kdeglobals` | `kconfig.config_path` (default `/etc/bor/xdg`) |
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
3. On the **Configuration** step, either pick a **preset** (**Security
   baseline**, **Shared lab workstation**, **Public kiosk**, **Branding
   defaults**) to add a ready-made set of settings you can then adjust, or
   use **Search settings** (label, key, file name or Windows policy name)
   and the tree on the left. For a catalogue setting the right side shows:
    - the file, group and key it writes, its explanation and the Windows
      equivalent;
    - **Value**: a **Restricted / Allowed** switch for Kiosk restrictions,
      an **Enabled / Disabled** switch for other booleans, a drop-down for
      enumerations, a number field with its range, a text field, a list
      (one item per line) or a colour picker (stored as `R,G,B`);
    - **Expand $VARIABLES** for text values, which writes KDE's `[$e]`
      marker so `$HOME` and other variables are expanded on the desktop;
    - **Enforced (Immutable)**: when on (**Enforced [$i]**), the entry is
      written with KDE's immutability marker and users cannot override it.
      When off, the value is a system default that users may change.
      Kiosk restrictions and security settings start enforced;
    - **Add to policy** / **Remove from policy**.
4. **System Settings modules** has its own editor: choose **Hide the
   listed modules** or **Show only the listed modules** (every other module
   installed on a node is hidden there), then pick modules from **Add
   module** or enter module ids under **Add custom module ids**.
5. **URL restrictions** has a rule editor and ready-made rule sets (**Home
   and temp folders only**, **No Windows shares**, **No FTP, SFTP or
   FISH**). Click **+ Add rule** and set **Action** (`open`, `list` or
   `redirect`), **Protocol**, **Host**, **Path** and **Access** (**Allow**
   or **Deny**). Blank fields match everything; `$HOME` and `$TMP` are
   expanded in paths. **Referrer matching (advanced)** restricts the rule to
   requests coming from a given protocol, host or path.
6. **Custom Entries > Add a custom entry** writes any other KDE key: file
   name, group (`[General]` or nested `[Containments][1]`), key, value and
   the `[$i]`, `[$e]`, `[$d]` and locale markers. **Advanced > Import INI**
   pastes a whole KDE configuration file prepared on a reference desktop;
   keys the catalogue knows are recognised. **Preview rendered files**
   shows exactly what the agent will write. **Lock the whole kdeglobals
   file** writes a file-level `[$i]`, which makes every user's own
   `kdeglobals` ignored; use it for kiosk desktops only.
7. Review, create and release the policy, then bind it to a node group.

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
| **Lock desktop layout (panels, widgets, favourites)** | Restricted | Enforced [$i] |

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

**System Settings modules**, mode **Hide the listed modules**: add
`kcm_autostart` (Autostart Applications), `kcm_kded` (Background Services)
and `kcm_krdpserver` (Remote Desktop).

`/etc/bor/xdg/kdeglobals` then also contains:

```text
[KDE Control Module Restrictions][$i]
kcm_autostart=false
kcm_kded=false
kcm_krdpserver=false
```

The modules disappear from System Settings and cannot be opened with
`kcmshell6` (or `kcmshell5`). With **Show only the listed modules**, the
listed modules are written as `true` and every other module the node has
installed as `false`.

### Deny browsing Windows shares

**URL restrictions**, rule set **No Windows shares** (or **+ Add rule**:
**Action** `list`, **Protocol** `smb`, **Access** **Deny**). Bor writes:

```text
[KDE URL Restrictions][$i]
rule_1=list,,,,smb,,,false
rule_2=open,,,,smb,,,false
rule_count=2
```

### A custom entry with variable expansion

**Custom Entries > Add a custom entry**: file `dolphinrc`, group
`[General]`, key `HomeUrl`, value `$HOME/Documents`, **Expand variables**
on, **Enforced** on.

```text
[General]
HomeUrl[$ie]=$HOME/Documents
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
- System Settings module lists are combined. A **show only** list written
  by a higher-priority policy keeps its modules visible even when a
  lower-priority policy hides them.

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

Many settings are only read when Plasma or the application starts; the
editor's explanation says whether a logout or an application restart is
needed.

## Compliance

Every setting is reported as its own item on the compliance page:
**Compliant** once it is written to the managed file, **Inapplicable** when
a higher-priority policy overrides it, and **Error** when a value could not
be written (for example a value containing a line break, or a show-only
module list on a node whose installed modules could not be listed). A
policy that only contains legacy settings is reported as **Inapplicable**
with an explanation.

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
kreadconfig6 --file dolphinrc --group General --key ShowFullPath

# System Settings modules installed on this node (ids for module restrictions)
kcmshell6 --list
```

On Plasma 5 use `kreadconfig5` and `kcmshell5`.

## Troubleshooting

**Nothing changes on the desktop.**

- Make sure the user logged in again after the first policy arrived, so
  that the hook scripts set `XDG_CONFIG_DIRS`. Check with
  `echo $XDG_CONFIG_DIRS` in a terminal of the session (if one is
  allowed), or `cat /proc/<pid-of-plasmashell>/environ | tr '\0' '\n'`.
- Compliance for this type confirms the files were written and which
  policy won each key; it does not read values back from the session yet.
- A setting marked **Plasma 6** has no effect on a Plasma 5 node.

**A setting is shown but users can still change it.** Turn on **Enforced
(Immutable)** for that setting. Non-enforced values are defaults only.

**System Settings still shows a restricted module.** Check that the module
ID is correct (it must match the module's ID, for example `kcm_autostart`;
`kcmshell6 --list` prints them) and that `/etc/bor/xdg/kdeglobals`
contains it. Restart System Settings. On a very old KDE Frameworks release
that does not read restrictions from `kdeglobals`, set
`kconfig.write_kde5rc: true` in the agent configuration so the agent also
mirrors them into `/etc/kde5rc`.

**A policy reports Inapplicable.** Either every setting in it is
overridden by a higher-priority policy (the item messages name it), or the
policy only contains legacy settings; open it and save it again.

**Files were changed back after editing them.** Files under `/etc/bor/xdg`,
the hook scripts and `/etc/kde5rc` (when mirrored) are tamper-protected.
The agent restores them and records a `tamper_detected` event in the
[audit log](../user-guide/audit-log.md).
