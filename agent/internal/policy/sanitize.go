// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"regexp"
)

// safeIdentifierRE matches identifiers that are safe to embed in a filesystem
// path component: letters, digits, dot, underscore and hyphen only. It
// deliberately excludes path separators ("/", "\") and any sequence that could
// be interpreted as a parent-directory reference.
var safeIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validatePathIdentifier verifies that a server-supplied identifier (e.g. a
// dconf database name, a repository id, or a policy id) is safe to use as a
// single path component. The agent runs as root and writes managed files into
// system directories, so an unvalidated identifier containing "/" or ".."
// would allow a (malicious or compromised) server to direct writes outside the
// intended directory. kind is used only for the error message.
func validatePathIdentifier(kind, id string) error {
	if id == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if id == "." || id == ".." {
		return fmt.Errorf("%s %q is not a valid path component", kind, id)
	}
	if !safeIdentifierRE.MatchString(id) {
		return fmt.Errorf("%s %q contains characters that are not permitted in a path component", kind, id)
	}
	return nil
}

// isValidPathIdentifier reports whether id is safe to use as a single path
// component (see validatePathIdentifier).
func isValidPathIdentifier(id string) bool {
	return id != "" && id != "." && id != ".." && safeIdentifierRE.MatchString(id)
}
