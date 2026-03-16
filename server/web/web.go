// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package web embeds the frontend static files for serving.
package web

import "embed"

// StaticFiles contains the embedded frontend static files.
//
//go:embed static/*
var StaticFiles embed.FS
