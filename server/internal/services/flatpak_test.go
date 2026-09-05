// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

const validFlatpakPolicy = `{
  "remotes": [{
    "name": "flathub", "url": "https://dl.flathub.org/repo/", "title": "Flathub",
    "enabled": true, "gpgVerify": true, "gpgKeyData": "bWFnaWM=", "subset": "verified",
    "priority": 2, "filterMode": "FLATPAK_FILTER_MODE_ALLOWLIST",
    "filterRefs": ["app/org.mozilla.*", "org.signal.Signal/*/stable"], "collectionId": "org.flathub.Stable"
  }],
  "apps": [
    {"appId": "org.mozilla.firefox", "remote": "flathub", "state": "FLATPAK_APP_STATE_PRESENT"},
    {"appId": "org.gimp.GIMP", "remote": "flathub", "branch": "stable", "state": "FLATPAK_APP_STATE_LATEST", "optional": true},
    {"appId": "com.example.Old", "state": "FLATPAK_APP_STATE_ABSENT", "deleteData": true}
  ],
  "autoUpdate": true, "autoUpdateIntervalHours": 12, "uninstallUnused": true, "operationTimeoutMinutes": 45
}`

func TestValidateFlatpakContent_Valid(t *testing.T) {
	if err := ValidateFlatpakContent(validFlatpakPolicy); err != nil {
		t.Fatalf("expected valid policy, got: %v", err)
	}
	// Remote-only and app-only policies are both acceptable.
	if err := ValidateFlatpakContent(`{"remotes":[{"name":"r","url":"https://x.example/repo/","gpgVerify":false}]}`); err != nil {
		t.Fatalf("remote-only policy rejected: %v", err)
	}
	if err := ValidateFlatpakContent(`{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"}]}`); err != nil {
		t.Fatalf("app-only policy rejected: %v", err)
	}
}

func TestValidateFlatpakContent_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"empty", "", "empty"},
		{"braces", "{}", "empty"},
		{"not json", "{nope", "invalid JSON"},
		{"nothing to manage", `{"autoUpdate":true}`, "at least one remote"},
		{"bad remote name", `{"remotes":[{"name":"../etc","url":"https://x/","gpgVerify":false}]}`, "invalid remote name"},
		{"http url", `{"remotes":[{"name":"r","url":"http://x/","gpgVerify":false}]}`, "only https"},
		{"file url", `{"remotes":[{"name":"r","url":"file:///tmp","gpgVerify":false}]}`, "only https"},
		{"gpg without key", `{"remotes":[{"name":"r","url":"https://x/","gpgVerify":true}]}`, "no gpg_key_data"},
		{"bad fingerprint", `{"remotes":[{"name":"r","url":"https://x/","gpgVerify":false,"gpgKeyId":"abc"}]}`, "40-character"},
		{"bad filter ref", `{"remotes":[{"name":"r","url":"https://x/","gpgVerify":false,"filterMode":"FLATPAK_FILTER_MODE_DENYLIST","filterRefs":["app/x;rm -rf"]}]}`, "invalid filter ref"},
		{"refs without mode", `{"remotes":[{"name":"r","url":"https://x/","gpgVerify":false,"filterRefs":["app/x"]}]}`, "filter_mode is NONE"},
		{"dup remote", `{"remotes":[{"name":"r","url":"https://x/","gpgVerify":false},{"name":"r","url":"https://y/","gpgVerify":false}]}`, "duplicate remote"},
		{"bad app id", `{"apps":[{"appId":"firefox","state":"FLATPAK_APP_STATE_PRESENT"}]}`, "invalid app_id"},
		{"app id traversal", `{"apps":[{"appId":"org..evil.App","state":"FLATPAK_APP_STATE_PRESENT"}]}`, "invalid app_id"},
		{"app no state", `{"apps":[{"appId":"org.example.App"}]}`, "state must be"},
		{"bad branch", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT","branch":"sta ble"}]}`, "invalid branch"},
		{"dup app", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"},{"appId":"org.example.App","state":"FLATPAK_APP_STATE_ABSENT"}]}`, "duplicate application"},
		{"bad interval", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"}],"autoUpdateIntervalHours":500}`, "auto_update_interval_hours"},
		{"bad timeout", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"}],"operationTimeoutMinutes":1}`, "operation_timeout_minutes"},
		{"bad installation", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"}],"installation":"a/b"}`, "invalid installation"},
		{"bad language", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT"}],"languages":["english"]}`, "invalid language"},
		{"bad commit", `{"apps":[{"appId":"org.example.App","state":"FLATPAK_APP_STATE_PRESENT","commit":"abc"}]}`, "commit must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFlatpakContent(tc.content)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}
