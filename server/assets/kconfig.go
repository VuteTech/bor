// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package assets

import _ "embed"

// KConfigBuiltinCatalogueJSON is the embedded built-in catalogue of KDE
// settings (a JSON array of KConfigCatalogueEntry in protojson form). Key
// names and value sets were taken from the KDE Kiosk documentation and
// from the KConfigXT schemas of Plasma 6.7; entries that only apply to one
// Plasma major version say so in plasmaMajor.
//
// The web UI consumes the same file through
// server/web/frontend/src/generated/kconfig_catalogue.ts, regenerated with
// `make kconfig-catalogue`.
//
//go:embed kconfig_builtin_catalogue.json
var KConfigBuiltinCatalogueJSON []byte
