<!-- SPDX-License-Identifier: LGPL-3.0-or-later -->

# Policy-type icons

Flat, single-colour marks that identify a policy type in the UI (create
wizard tiles, the policies list, filters, the dashboard). Every icon renders
through PatternFly's `createIcon`, so it behaves like any
`@patternfly/react-icons` icon: `fill="currentColor"`, `1em` box,
`aria-hidden` unless a `title` is passed. Use them through
`components/PolicyTypeIcon` rather than importing a mark directly.

## Sources and licences

| Policy type | Component | Artwork | Licence | Trademark holder |
| --- | --- | --- | --- | --- |
| Firefox | `FirefoxMarkIcon` | Simple Icons `firefox` v16.29.0 | CC0-1.0 (`LICENSE.CC0-1.0.md`) | Mozilla Foundation |
| Thunderbird | `ThunderbirdMarkIcon` | Simple Icons `thunderbird` v16.29.0 | CC0-1.0 (`LICENSE.CC0-1.0.md`) | MZLA Technologies Corporation / Mozilla Foundation |
| Chrome | `ChromeMarkIcon` | Simple Icons `googlechrome` v16.29.0 | CC0-1.0 (`LICENSE.CC0-1.0.md`) | Google LLC |
| Edge | `EdgeIcon` from `@patternfly/react-icons` | Font Awesome Free 5 brand icon, bundled by PatternFly (Simple Icons carries no Edge mark) | MIT (`@patternfly/react-icons`); Font Awesome Free icons CC BY 4.0, attribution carried by the package | Microsoft Corporation |
| Kconfig (KDE Plasma) | `KdeMarkIcon` | Simple Icons `kde` v16.29.0 | CC0-1.0 (`LICENSE.CC0-1.0.md`) | KDE e.V. |
| Dconf | `SlidersHIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — (dconf is not tied to one desktop, so no desktop logo is used) |
| Polkit | `KeyIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — |
| Firewalld | `ShieldAltIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — |
| SessionAccess | `UserClockIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — |
| Package | `CubesIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — |
| unknown type | `CogIcon` (generic) | `@patternfly/react-icons` | MIT / CC BY 4.0 as above | — |

The Simple Icons artwork is dedicated to the public domain under CC0 1.0;
the full text is in `LICENSE.CC0-1.0.md`. Each `*MarkIcon.tsx` file repeats
its source URL, version and licence in its header so the provenance travels
with the code.

## Trademarks

CC0 covers the *artwork*, not the *brand*. The marks remain trademarks of
their owners and are used here in the nominative sense only: to tell the
administrator which product a policy configures. They are rendered in the
UI's icon colour, never altered, never used as Bor branding, and never
suggest endorsement. Simple Icons asks users to read its disclaimer and the
owners' brand guidelines before use:
https://github.com/simple-icons/simple-icons/blob/develop/DISCLAIMER.md

## Adding a mark

1. Download the SVG from the Simple Icons repository (or another CC0/MIT
   source) and record the version.
2. Copy the `d` attribute into a new `<Name>MarkIcon.tsx` using the same
   header and `createIcon({ width: 24, height: 24, svgPath })` shape.
3. Add the row above and register the icon in `policyTypes/registry.tsx`.
