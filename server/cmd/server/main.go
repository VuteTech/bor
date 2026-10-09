// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Command server is the Bor community edition server.
package main

import (
	"flag"
	"log"

	"github.com/VuteTech/Bor/server/pkg/app"
	"github.com/VuteTech/Bor/server/pkg/edition"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

func main() {
	resetMFAUser := flag.String("reset-mfa", "", "Disable MFA for the given username and exit")
	flag.Parse()

	if *resetMFAUser != "" {
		if err := app.ResetMFA(*resetMFAUser); err != nil {
			log.Fatalf("reset-mfa failed: %v", err)
		}
		return
	}

	app.Run(app.Options{Version: Version, Edition: edition.Community{}})
}
