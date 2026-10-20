// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"embed"
	"encoding/json"
	"log"
	"sync"
)

//go:embed macdata/selinux_booleans.json macdata/apparmor_profiles.json
var macDataFS embed.FS

// CuratedBoolean describes an SELinux boolean that matters on desktops.
// Nodes report names and values but no usable descriptions without the
// policy development package, so the server carries them.
type CuratedBoolean struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	// Restrictive is the value that hardens the system, when there is one.
	Restrictive *bool `json:"restrictive,omitempty"`
	// Risky values deserve a confirmation in the editor.
	Risky bool `json:"risky,omitempty"`
}

// CuratedProfile describes an AppArmor profile file common on desktops.
type CuratedProfile struct {
	File        string `json:"file"`
	Description string `json:"description"`
	Protected   bool   `json:"protected,omitempty"`
}

var (
	macDataOnce      sync.Once
	curatedBooleans  []CuratedBoolean
	curatedProfiles  []CuratedProfile
	curatedBoolIndex map[string]*CuratedBoolean
	curatedProfIndex map[string]*CuratedProfile
)

func loadMacData() {
	macDataOnce.Do(func() {
		if data, err := macDataFS.ReadFile("macdata/selinux_booleans.json"); err == nil {
			if err := json.Unmarshal(data, &curatedBooleans); err != nil {
				log.Printf("WARNING: curated SELinux booleans: %v", err)
			}
		}
		if data, err := macDataFS.ReadFile("macdata/apparmor_profiles.json"); err == nil {
			if err := json.Unmarshal(data, &curatedProfiles); err != nil {
				log.Printf("WARNING: curated AppArmor profiles: %v", err)
			}
		}
		curatedBoolIndex = make(map[string]*CuratedBoolean, len(curatedBooleans))
		for i := range curatedBooleans {
			curatedBoolIndex[curatedBooleans[i].Name] = &curatedBooleans[i]
		}
		curatedProfIndex = make(map[string]*CuratedProfile, len(curatedProfiles))
		for i := range curatedProfiles {
			curatedProfIndex[curatedProfiles[i].File] = &curatedProfiles[i]
		}
	})
}

// CuratedSelinuxBooleans returns the curated boolean list.
func CuratedSelinuxBooleans() []CuratedBoolean {
	loadMacData()
	return curatedBooleans
}

// CuratedSelinuxBoolean returns the curated entry of a boolean, or nil.
func CuratedSelinuxBoolean(name string) *CuratedBoolean {
	loadMacData()
	return curatedBoolIndex[name]
}

// CuratedApparmorProfiles returns the curated profile list.
func CuratedApparmorProfiles() []CuratedProfile {
	loadMacData()
	return curatedProfiles
}

// CuratedApparmorProfile returns the curated entry of a profile file, or
// nil.
func CuratedApparmorProfile(file string) *CuratedProfile {
	loadMacData()
	return curatedProfIndex[file]
}
