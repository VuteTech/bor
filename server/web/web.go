// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package web embeds the frontend static files for serving.
package web

import "embed"

// StaticFiles contains the embedded frontend static files.
//
//go:embed static/*
var StaticFiles embed.FS
