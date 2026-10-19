// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package systemd

import (
	"io"
	"strings"
)

func stringsReader(s string) io.Reader { return strings.NewReader(s) }
