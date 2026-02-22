// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"fmt"
)

// ValidateChromeContent validates that the given JSON string is a valid Chrome policy object.
// Returns an error if the JSON is malformed or not a JSON object, or if the content is empty.
func ValidateChromeContent(content string) error {
	if content == "" || content == "{}" {
		return fmt.Errorf("chrome policy content is empty")
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return fmt.Errorf("invalid chrome policy JSON: %w", err)
	}

	for k := range obj {
		if k == "" {
			return fmt.Errorf("chrome policy keys must be non-empty strings")
		}
	}

	return nil
}
