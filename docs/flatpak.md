<!-- SPDX-License-Identifier: LGPL-3.0-or-later -->

# Flatpak Policies

The **Flatpak** policy type manages Flatpak on agent nodes: which **remotes**
(repositories) are configured, which **applications** must be installed,
kept up to date or removed, and how updates are handled. The server keeps a
searchable **application catalog** (Flathub out of the box, more remotes on
request) so applications are picked by name instead of typed by ID.

Typical uses: a standard desktop image where Firefox, LibreOffice and a video
call client come from Flathub and stay updated; a lab where only a verified
allow-list of applications may be installed; removing an application from an
entire fleet.

## What is Flatpak?

[Flatpak](https://flatpak.org) is a distribution-independent application
format for Linux desktops. Applications are published in **remotes**
(OSTree repositories such as [Flathub](https://flathub.org)), identified by a
reverse-DNS ID such as `org.mozilla.firefox`, and installed either
**system-wide** (`/var/lib/flatpak`) or **per user**
(`~/.local/share/flatpak`). A remote can be restricted with a **subset**
(Flathub offers `verified`, `floss` and `verified_floss`) and with a local
**filter** of allowed or denied refs.

## How enforcement works

The agent runs as root and drives the `flatpak` command line with fixed,
validated arguments (there is no stable D-Bus installation API). Every sync:

1. **Remotes** — for each remote in the policy the agent writes
   `/etc/bor/flatpak/<name>.flatpakrepo` (plus `<name>.filter` and
   `<name>.gpg` when a filter or key is configured), then runs
   `flatpak remote-add --from` for a missing remote or the minimal
   `flatpak remote-modify` flags for one that already exists (a distribution
   pre-configured `flathub` is *adopted*, not duplicated). These files are
   tamper-protected: if a local administrator edits them the agent restores
   them and reports a tamper event.
2. **Applications** — the agent reads the installed applications with
   `flatpak list`, then runs only the operations needed to reach the desired
   state (`flatpak install`, `flatpak install --or-update` for *Latest*,
   `flatpak uninstall` for *Absent*) and re-checks afterwards. Operations are
   idempotent and run off the policy stream, so a long download never delays
   other policies.
3. **Updates and cleanup** — with *Auto update* enabled the agent runs
   `flatpak update` on the configured interval (default every 24 hours);
   *Uninstall unused runtimes* runs `flatpak uninstall --unused` after
   changes.

When the last Flatpak policy is unbound from a node, remotes that Bor created
are removed (unless installed applications still depend on them), adopted
remotes get their original settings back, and the managed files are deleted.
**Installed applications are left in place** — removal is always an explicit
*Absent* entry.

## Node requirements

- The `flatpak` package. Nodes without it report the policy as
  **Inapplicable**; nothing else is required.
- Network access from the node to the remote's URL (for Flathub,
  `dl.flathub.org`). Set `flatpak.proxy_url` in the agent configuration when
  downloads must go through an HTTP proxy.
- Only the **system** installation (or a named system installation) is
  enforced by this release. Entries with the *User* scope are accepted by the
  server and reported as Inapplicable by the agent.

## Compliance states

The agent reports one item per remote (`flatpak:remote`) and one per
application (`flatpak:app`); the policy status is the roll-up of all items.

| State | Meaning |
|---|---|
| **Compliant** | Every remote is configured as requested and every application is in its desired state. |
| **Non-Compliant** | An application is missing from its remote (and not marked *optional*), or a remote has GPG verification disabled (a permanent reminder), or a remote setting could not be expressed with `flatpak remote-modify`. |
| **Inapplicable** | `flatpak` is not installed, the named installation does not exist, the entry uses the *User* scope, or an *optional* application is not available in its remote. |
| **Error** | An install, uninstall or remote operation failed (network, GPG, lock held by another `flatpak` process, timeout). The message carries the last line of `flatpak`'s output. |

## Server catalog (Settings → Flatpak repositories)

The catalog is a **server setting**, separate from policies. Each entry is a
remote the server indexes:

- **Flathub** is seeded and refreshed daily from
  `https://dl.flathub.org/repo/appstream/x86_64/appstream.xml.gz`
  (about 10 MB; roughly 3,300 desktop applications). It cannot be deleted,
  only disabled.
- **Add repository** accepts a `.flatpakrepo` URL (fields are imported,
  including the GPG key), a preset (Flathub Beta), or manual fields. A
  repository is indexed only if it publishes an AppStream catalog over HTTPS
  (`<url>/appstream/<arch>/appstream.xml.gz`, or an explicit *AppStream URL*).
- **Refresh now** re-indexes immediately; **Upload catalog…** imports an
  `appstream.xml.gz` downloaded elsewhere — the path for air-gapped servers.
- The server-wide switch `BOR_FLATPAK_CATALOG_REFRESH=false` disables all
  outbound catalog fetches; uploads keep working.

The catalog feeds the policy editor's search and provides ready-made remote
definitions. Policies stay **self-contained**: adding a remote "from server
repository" copies its URL, key and subset into the policy, so agents never
depend on the server's catalog settings and exported policies carry
everything they need.

Permissions: `flatpak_repo:view|create|edit|delete|refresh` (granted to every
role that already had `settings:manage`). Searching the catalog from the
policy editor needs only `policy:view`.

## Creating a Flatpak policy

1. **Remotes** — add at least the remote your applications come from. *From
   server repository* copies a catalog entry; *From .flatpakrepo URL* fetches
   and parses a repository file; *Custom* lets you type everything. Per remote
   you can set the subset (for example `verified`), a filter (allow-list or
   deny-list of refs — an allow-list automatically keeps runtimes and the
   policy's own applications installable), priority, enabled state, and
   whether app stores may list it.
2. **Applications** — search the catalog, then *Add*. Each row has a desired
   **state** (*Present*, *Latest*, *Absent*), the **remote** to install from
   (always name it when more than one remote could provide the app), an
   optional **branch**, and the **optional** flag that turns "not available
   on this node" into Inapplicable instead of Non-Compliant. Applications
   from remotes the server does not index are added by ID.
3. **Options** — *Auto update* and its interval, *Uninstall unused
   runtimes*, the per-operation timeout (default 30 minutes), and — under
   *Advanced* — a named system installation.
4. Release the policy and bind it to node groups as usual. Several Flatpak
   policies bound to one node are merged: remotes by name, applications by
   ID, with the higher binding priority winning; *Auto update* is on if any
   policy enables it.

### Restricting what local users may install

Flatpak's polkit actions gate **system-wide** changes. To require an
administrator password for every local install, uninstall or remote change,
create a **Polkit** policy with the result `auth_admin` for
`org.freedesktop.Flatpak.app-install`, `org.freedesktop.Flatpak.runtime-install`,
`org.freedesktop.Flatpak.app-uninstall`, `org.freedesktop.Flatpak.runtime-uninstall`,
`org.freedesktop.Flatpak.modify-repo`, `org.freedesktop.Flatpak.configure-remote`
and `org.freedesktop.Flatpak.configure` (by default members of the
distribution's administrator group are allowed without a prompt). Combine it
with an allow-list filter on the remote to limit *what* can be installed at
all. Note that nothing in Flatpak prevents a user from installing into their
own per-user installation.

## Example policies

**1 — Standard desktop applications from Flathub, kept current**

Remote `flathub` from the server repository (subset `verified`), applications
`org.mozilla.firefox`, `org.libreoffice.LibreOffice` and
`com.github.IsmaelMartinez.teams_for_linux` with state *Latest*, *Auto
update* every 24 hours, *Uninstall unused runtimes* on.

**2 — Locked-down lab**

Remote `flathub` with an allow-list filter containing `app/org.gnome.gedit`,
`app/org.gimp.GIMP` and `app/org.inkscape.Inkscape`, priority 10, the three
applications *Present*, plus a Polkit policy as described above.

**3 — Remove an application fleet-wide**

No remotes, one application `com.example.LegacyTool` with state *Absent* and
*Delete data* enabled. Nodes without Flatpak report Inapplicable, everything
else removes the app on the next sync.

## Compliance dashboard

Expand a row on the Compliance page to see the per-remote and per-application
items. A *Non-Compliant* remote item with the message "GPG verification
disabled" is intentional and persists until verification is re-enabled.

## Operational notes

- **Timeouts.** Installs are bounded by *Operation timeout* (5–240 minutes).
  A timed-out operation reports *Error* and is retried on the next sync;
  Flatpak resumes partial downloads and deploys atomically, so an interrupted
  install never leaves a half-installed application.
- **Concurrent use.** If a local user runs `flatpak` at the same time the
  agent waits for the installation lock up to the timeout.
- **Ambiguous remotes.** With `--noninteractive`, Flatpak refuses to guess when
  several remotes provide the same ref; always name the remote on the
  application entry.
- **Locale extensions.** Flatpak downloads language packs for the languages
  configured on the node (`flatpak config languages`); this is not managed by
  the policy in this release.
- **Distribution-managed remotes** (for example Fedora's `fedora` remote) are
  untouched unless a policy names them.
- **Agent restarts** during an install cancel the running `flatpak` process
  cleanly (`TimeoutStopSec=120` in the unit); the next sync finishes the work.
- Everything the agent does is logged to the journal (`journalctl -u bor-agent`).
