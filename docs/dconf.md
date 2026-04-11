# dconf Policies

Bor can enforce GSettings / dconf settings as mandatory system policies on Linux desktops running GNOME or any other desktop that reads GSettings.

## What is dconf / GSettings?

[dconf](https://wiki.gnome.org/Projects/dconf) is the low-level key-value store behind GSettings, the GNOME settings API. Applications read settings through `g_settings_get_*()`. Mandatory system policies are delivered by writing INI keyfiles under `/etc/dconf/db/` and running `dconf update` — already-running sessions pick up the changes immediately via D-Bus.

## Compliance states

dconf compliance uses four states rather than a simple boolean:

| State | Meaning | Counted in score? |
|---|---|---|
| `COMPLIANT` | Key present and matches the policy value | Yes (numerator + denominator) |
| `NON_COMPLIANT` | Key present but value does not match | Yes (denominator only) |
| `INAPPLICABLE` | Schema not installed on this node | No — excluded entirely |
| `ERROR` | Schema installed but enforcement or check failed | Yes (denominator only) |

```
score = COMPLIANT / (COMPLIANT + NON_COMPLIANT + ERROR)
```

`INAPPLICABLE` is excluded so that a KDE or headless node receiving a GNOME screensaver policy is not counted as non-compliant. A policy is rolled up as `INAPPLICABLE` only when every entry is inapplicable (GNOME not present on the node at all).

## Creating a dconf policy

1. Go to **Policies** and create a new policy with type **Dconf**.
2. Click **Add entry** for each GSettings key to enforce:
   - **Schema** — searchable dropdown (e.g. `org.gnome.desktop.screensaver`).
   - **Key** — keys for the selected schema with a summary subtitle.
   - **Value** — type-aware input widget (see table below).
   - **Lock** — when checked, users cannot override the setting.
3. Set the **dconf database name** if you need to write to a database other than `local`.
4. Release the policy and create an enabled binding to a node group.

### GVariant value format

The editor formats values automatically based on the key type. When editing raw JSON, values must be in [GVariant text format](https://docs.gtk.org/glib/gvariant-text-format.html):

| Type code | Example | Notes |
|---|---|---|
| `b` | `true` or `false` | |
| `i` | `5` | int32 — no prefix needed |
| `u` | `uint32 300` | uint32 requires the prefix |
| `q` | `uint16 5042` | uint16 requires the prefix |
| `d` | `1.5` | double |
| `s` | `'Adwaita'` | string — single quotes required |
| `as` | `['first', 'second']` | string array |

A bare integer (e.g. `5042`) for a `uint16` key causes dconf to silently ignore the entry. The policy editor prevents this by formatting values automatically.

## Compliance dashboard

The **Compliance** page shows one row per (node, policy) pair. Only results for policies with at least one **enabled** binding covering the node are shown — disabling or removing a binding removes the row immediately.

Rows with dconf policies have an expand toggle showing a per-key breakdown:

| Column | Description |
|---|---|
| Key | Human-readable summary from the schema catalogue; raw key name as fallback |
| Status | `COMPLIANT` / `NON_COMPLIANT` / `INAPPLICABLE` / `ERROR` with colour coding |
| Details | For non-compliant keys: expected and actual GVariant values |

## Operational notes

- **Relocatable schemas** require a `dconf path override` in the editor. The path must begin and end with `/`.
- **Schema catalogue freshness** — the agent rescans installed schemas on every restart. Package installs between restarts are not reflected until the agent restarts.
- **Flatpak apps** with modern runtimes receive host mandatory settings for `org.gnome.desktop.*` via the `org.freedesktop.portal.Settings` D-Bus portal. App-specific schemas are not relayed.
- **Immutable-root systems** (openSUSE MicroOS / Aeon / Kalpa) — `/etc/dconf/` sits on the read-write overlay layer; `dconf update` should work but is untested.
- **SELinux on RHEL / Fedora** — writing to `/etc/dconf/db/` from a root system service may require a custom SELinux policy module.
