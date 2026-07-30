// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import "strings"

// csvFormulaPrefixes are the leading characters a spreadsheet application
// (Excel, LibreOffice Calc, Google Sheets) may interpret as the start of a
// formula. A leading tab or carriage return is included because it can smuggle
// a formula past a naive first-character check once the cell is trimmed.
const csvFormulaPrefixes = "=+-@\t\r"

// sanitizeCSVField neutralises spreadsheet formula injection ("CSV injection").
// encoding/csv already handles RFC 4180 quoting; this guards the separate risk
// that an exported value is executed as a formula when the file is opened in a
// spreadsheet. A field beginning with a formula prefix is made literal by
// prepending a single quote. It mirrors the frontend nodes-export guard so both
// exports behave identically.
func sanitizeCSVField(v string) string {
	if v != "" && strings.IndexByte(csvFormulaPrefixes, v[0]) >= 0 {
		return "'" + v
	}
	return v
}
