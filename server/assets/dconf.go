// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package assets embeds static server assets.
package assets

import _ "embed"

// DConfBuiltinSchemasJSON is the embedded built-in GSettings schema catalogue
// generated from a reference Fedora 43 / GNOME 49 installation.
// It is used to seed the dconf_schemas table at server startup.
//
//go:embed dconf_builtin_schemas.json
var DConfBuiltinSchemasJSON []byte
