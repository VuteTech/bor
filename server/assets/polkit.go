// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package assets

import _ "embed"

// PolkitBuiltinActionsJSON is the embedded built-in polkit action catalogue
// generated from a reference Manjaro / polkit installation via gen-polkit-actions.
// It is used to seed the polkit_actions table at server startup so that admins
// can create Polkit policies before any agent has reported its catalogue.
//
//go:embed polkit_builtin_actions.json
var PolkitBuiltinActionsJSON []byte
