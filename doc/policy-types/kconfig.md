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
| Text Editor (Kate) | Kate and KWrite: sessions, backups, swap files, indentation, encoding, line numbers, word wrap, clipboard and search history kept, colour theme, font, which LSP language servers may start | `katerc` |
| Screenshots (Spectacle) | What the Print key does, saving and clipboard behaviour, pointer and decorations, screenshot and recording folders, file formats and name templates | `spectaclerc` |
| KDE PIM | KMail composer and reader behaviour, HTML mail and external references, scam and phishing detection, read receipts, encryption of drafts and sent mail; KOrganizer, Akregator and Kontact defaults | `kmail2rc`, `mailviewerrc`, `korganizerrc`, `akregatorrc`, `kontactrc` |
| Notifications & Feedback | Low disk space warning and thresholds, launch feedback (busy cursor, task bar entry) | `freespacenotifierrc`, `klaunchrc` |
| Applications | A Konsole terminal profile deployed to the nodes, applications hidden from menus and launchers | `konsole/<Name>.profile`, `applications/<id>.desktop` in the data directory |
| Custom Entries | Any other KDE configuration key: file, group, key, value and markers | the file you name |
| Scope | Limit the whole policy to members of Unix groups (local, LDAP or Active Directory) | the same files, under `groups/<group>/` |
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

!!! note "Desktop wallpaper"
    The catalogue has no plain desktop wallpaper key on purpose: the Plasma
    layout file holds one wallpaper per desktop, and writing a fixed entry
    into it does not take effect reliably. Use **Desktop Layout >
    Wallpaper and panel layout** (below), which deploys the wallpaper the
    way KDE's own global themes do, and **Lock screen wallpaper image**
    (Screen Lock) for the lock screen.

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
| `/etc/bor/xdg/groups/<group>/<file>` | Files of policies scoped to a Unix group, activated for members by the hook scripts. The whole `groups/` directory belongs to Bor; files no longer needed are removed | `kconfig.config_path` |
| `/etc/bor/share/plasma/look-and-feel/org.bor.<policy>.desktop/`, `/etc/bor/share/plasma/shells/org.kde.plasma.desktop/contents/updates/bor-<hash>.js` | Generated look-and-feel package (wallpaper, panel layout) and the apply-once update script of the Desktop Layout feature. The whole directory belongs to Bor; files no longer needed are removed | `kconfig.data_path` (default `/etc/bor/share`) |
| `/etc/bor/share/konsole/<Name>.profile`, `/etc/bor/share/applications/<id>.desktop` | Konsole profile and the desktop entries of hidden applications (copies of the installed entries with `NoDisplay=true`). Group-scoped policies write theirs under `/etc/bor/share/groups/<group>/` | `kconfig.data_path` |
| Flatpak system override (`flatpak override --system --show`) | Exposes `/etc/bor/xdg` to sandboxed applications and prepends it to their `XDG_CONFIG_DIRS` while KDE Plasma policies apply | `kconfig.flatpak_override` (default `true`) |
| `/etc/xdg/plasma-workspace/env/00-bor.sh` | Puts the Bor directories first in `XDG_CONFIG_DIRS` and `XDG_DATA_DIRS` for every Plasma session (sourced by `startplasma`, independent of the display manager) | Follows `kconfig.config_path` and `kconfig.data_path` |
| `/etc/profile.d/99-bor.sh` | Same, for login shells and display managers whose session scripts source `/etc/profile` | Follows `kconfig.config_path` and `kconfig.data_path` |
| `/etc/kde5rc` | Optional mirror of the System Settings module restrictions for very old KDE Frameworks releases; Plasma 5 and 6 read them from `kdeglobals` | `kconfig.write_kde5rc` (default `false`) |

Both hook scripts contain the same POSIX shell snippet:

```bash
# Group-scoped policy directories, highest priority first.
bor_group_dirs=""
bor_gids=" $(id -G 2>/dev/null) "
bor_group_data=""
case "$bor_gids" in *" 1002 "*) bor_group_dirs="${bor_group_dirs}/etc/bor/xdg/groups/teachers:"; bor_group_data="${bor_group_data}/etc/bor/share/groups/teachers:" ;; esac
unset bor_gids
case ":${XDG_CONFIG_DIRS:-/etc/xdg}:" in
  *":/etc/bor/xdg:"*) ;;
  *) export XDG_CONFIG_DIRS="${bor_group_dirs}/etc/bor/xdg:${XDG_CONFIG_DIRS:-/etc/xdg}" ;;
esac
unset bor_group_dirs
case ":${XDG_DATA_DIRS:-/usr/local/share:/usr/share}:" in
  *":/etc/bor/share:"*) ;;
  *) export XDG_DATA_DIRS="${bor_group_data}/etc/bor/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}" ;;
esac
unset bor_group_data
```

The group lines (everything that mentions `bor_group_dirs` or
`bor_group_data`) only appear while a policy scoped to a Unix group applies
(see *Scoping a policy to user groups*); on other nodes the snippet is the
two `case` blocks alone.

KDE searches `XDG_CONFIG_DIRS` for system-wide configuration, so the files
in `/etc/bor/xdg` take precedence over distribution defaults in `/etc/xdg`;
`XDG_DATA_DIRS` makes the generated look-and-feel package and Plasma
scripts visible. The variables are set when the session starts, so
**users must log out and back in** once after the first KDE Plasma policy
reaches a node.

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

### Settings reported by nodes

Every agent on a KDE node reports two things when it connects: the
settings described by the KConfigXT schemas installed under
`/usr/share/config.kcfg` (file, group, key, type, choices, default and
label for roughly 1,400 keys on a stock Plasma 6 installation, including
KDE applications such as Dolphin, Okular, Ark, KMail or KOrganizer) and
the System Settings modules it has installed, with their display names.

In the editor, **Settings reported by node** adds that node's settings to
the tree under groups named after the configuration file (**Node:
dolphinrc**, **Node: kmail2rc**, ...), marked **reported by a node**, and
replaces the module list of **System Settings modules** with the node's
own modules. Keys the built-in catalogue already describes keep their
curated label and explanation. Use a node that runs the Plasma version and
applications you manage; what it reports is what those nodes read.

Node-reported settings carry the label from the schema, which is often
terse, and no Windows equivalent. Their defaults are the KDE defaults for
the version installed on that node.

### Desktop layout and wallpaper

**Desktop Layout > Wallpaper and panel layout** deploys the desktop look
the way KDE's own global themes do. The agent generates a look-and-feel
package named after the policy (`org.bor.<policy>.desktop`) in the data
directory (`/etc/bor/share/plasma/look-and-feel/`, first in
`XDG_DATA_DIRS` through the same hook scripts) and selects it as the
global theme (`kdeglobals`, `[KDE]`, `LookAndFeelPackage`). The package
carries:

- the **Wallpaper image** (an absolute path readable by every user; deploy
  the file itself with a package) and its **fill mode**, in the package's
  `defaults`;
- an optional **Panel layout script**: Plasma desktop scripting
  (JavaScript) that creates panels and widgets, stored as the package's
  desktop layout. **Insert Plasma's default panel** gives the script
  Breeze uses as a starting point.

Users who log in for the first time get the wallpaper and the layout.
Three switches control everyone else:

- **Apply once to existing users** writes a Plasma update script into the
  data directory. Plasma runs it once per user at the next login: it sets
  the wallpaper on every desktop and, when a layout script is set,
  **replaces the user's panels** with the scripted layout. The script's
  file name changes with its content, so a changed wallpaper or layout is
  applied again.
- **Apply the wallpaper to logged-in sessions now** runs
  `plasma-apply-wallpaperimage` for every active session right after the
  agent writes the package.
- **Lock the global theme** writes the theme selection as enforced, so
  users cannot pick another global theme in System Settings.

Colours, icons, fonts and the cursor theme are not part of the generated
package; set them under **Appearance**. When several policies with a
layout reach a node, the one with the highest binding priority provides the
package and the others report it as inapplicable. A **Global theme**
setting from the Appearance category in a higher-priority policy keeps its
own theme selection; the package is still generated.

The **Branding defaults** preset sets up the theme defaults around the
wallpaper; add the wallpaper image under Desktop Layout afterwards.

### Konsole profile

**Applications > Konsole profile** deploys a terminal profile the way
distributions ship theirs. The agent writes `konsole/<Name>.profile` into
the data directory (`/etc/bor/share/konsole/`), where Konsole lists it next
to its built-in profiles. The profile sets the **command** the terminal
starts, the **scrollback** mode and size, the **colour scheme** and the
**font**; **Additional profile entries** take any other key of a Konsole
profile as INI text (`[Terminal Features]`, `[Interaction Options]`,
`[Cursor Options]`, ...). Everything the profile does not set is inherited
from Konsole's built-in defaults (`Parent=FALLBACK/`).

- **Use as default profile** writes `konsolerc`, `[Desktop Entry]`,
  `DefaultProfile=<Name>.profile`, so new windows and tabs use it. Without
  it the profile is only offered in Konsole's profile list.
- **Lock** writes that selection as enforced and adds the Kiosk
  restrictions `action/manage-profiles`, `action/edit-current-profile` and
  `action/switch-profile` to `kdeglobals`, which hide Konsole's profile
  management. Users can neither switch to another profile nor create or
  edit one.

Konsole reads profiles when it starts; open windows need a restart. A
profile of the same name that a user saved under `~/.local/share/konsole/`
*before* the policy arrived still takes precedence, because the user's data
directory comes first; pick a name users are unlikely to have used
("Managed" is the default). One profile per node: when several bound
policies carry one, the highest binding priority wins and the others report
theirs as inapplicable. A policy scoped to a Unix group gets its own
profile under `groups/<group>/konsole/` in the data directory. The
**Managed terminal** preset combines a locked profile without scrollback
with the Konsole settings that keep the terminal from being scripted from
outside.

### Hiding applications

**Applications > Hidden applications** removes applications from the
application menu, KRunner and other launchers. For every listed desktop
file id the agent copies the installed entry (from
`/usr/share/applications`, `/usr/local/share/applications` or the Flatpak
and Snap export directories) into `applications/` in the data directory and
adds `NoDisplay=true`; because that directory comes first in
`XDG_DATA_DIRS`, the copy shadows the original. When no installed entry
exists, a minimal hidden entry is written so the application stays hidden
after it gets installed.

Hiding is a menu clean-up, not a block: file associations keep working, and
the program can still be started from a terminal or a file manager. Use the
Kiosk restrictions under Session & Shell, System Settings module
restrictions, polkit rules or package removal to prevent use. Ids are the
file names under `/usr/share/applications` on the nodes
(`org.kde.konsole.desktop`, `systemsettings.desktop`, `firefox.desktop`);
the editor offers the common KDE ones. Lists from all bound policies are
combined. The change takes effect at the next login; the agent also
refreshes the application cache (`kbuildsycoca6`) of running sessions.
Policies scoped to a Unix group hide their applications for members only
(`groups/<group>/applications/` in the data directory). The **Hide
administration tools** preset hides System Settings, Info Center, the menu
editor, Discover, System Monitor, KSystemLog, Partition Manager, KWallet
Manager and Akonadi Console.

### Scoping a policy to user groups

Policies normally apply to every user of a node (the Windows "Computer
Configuration"). **Scope > Apply only to Unix groups** limits every
setting of the policy to members of the listed groups, which is how a lab
machine gives teachers and students different settings (the Windows "User
Configuration with security filtering"):

- The agent writes the policy's files into `groups/<group>/` under the
  managed directory (`/etc/bor/xdg/groups/students/kdeglobals`, ...)
  instead of the node-wide files. Several scoped policies for the same
  group merge by binding priority like node-wide ones.
- The login hook resolves each group to its numeric id on the node and,
  for members, adds the group directories to `XDG_CONFIG_DIRS` **before**
  the node-wide directory, highest priority first. Group names never reach
  the shell; `id -G` is matched on numbers.
- Names are resolved through the node's name service, so local groups,
  LDAP, SSSD and winbind groups all work. Use the names `id -Gn` prints on
  the nodes, including fully qualified ones such as
  `teachers@ad.example.com` or `Domain Users`. A group the node does not
  know is reported as an **Error** item (`groups/<group> / unix group`);
  its files are written but not activated until the next sync after the
  group becomes resolvable.
- KDE reads the less specific files first, so a group directory overrides
  node-wide **defaults** but cannot relax a node-wide **enforced** entry.
  Put the settings that differ between groups into scoped policies only,
  and keep the node-wide policies for what applies to everyone.
- The desktop layout (look-and-feel package) and System Settings module
  restrictions written in show-only mode apply to every user; a scoped
  policy that carries a layout reports it as inapplicable. Konsole
  profiles and hidden applications *can* be scoped: their files go to
  `groups/<group>/` under the data directory, which the hook adds to
  `XDG_DATA_DIRS` for members. Flatpak applications see only the
  node-wide settings (see below).

### Flatpak applications

KDE applications installed as Flatpaks (Okular, Kate, Dolphin from
Flathub) run with their own `/etc` and would never see the managed
directory. While at least one KDE Plasma policy applies and `flatpak` is
installed, the agent therefore adds a system-wide Flatpak override that
exposes the managed directory read-only and puts it first in the sandbox's
`XDG_CONFIG_DIRS` (`flatpak override --system --show` lists it). When the
last policy is unbound the agent removes the environment entry and
revokes the directory; Flatpak records the revocation as a negative
`filesystems=!/etc/bor/xdg` entry, which is harmless and disappears the
next time the override is granted. Running Flatpak applications pick the
change up after a restart. Disable this with `kconfig.flatpak_override:
false` in the agent configuration. Group-scoped settings do not reach
Flatpak applications.

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

With `kconfig.probe_effective: true` in the agent configuration, the agent
additionally reads every written setting back with `kreadconfig6` (or
`kreadconfig5`) in a logged-in user's session after each sync. A setting
whose effective value differs from the policy value is reported
**Non-compliant** with both values: that happens when a user's own
configuration overrides a setting that is not enforced. The agent also
checks that the running Plasma session has the managed directory in
`XDG_CONFIG_DIRS` and reports a **Non-compliant** `session /
XDG_CONFIG_DIRS` item until the user logs in again. The probe is off by
default; it runs read-only commands as the session user and skips nodes
without a graphical session.

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
