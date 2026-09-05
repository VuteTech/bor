// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// RepoFile is the parsed content of a .flatpakrepo file (flatpak-remote(5)).
type RepoFile struct {
	Title         string
	URL           string
	Homepage      string
	Comment       string
	Description   string
	Icon          string
	GPGKey        []byte
	DefaultBranch string
	Filter        string
	Subset        string
	CollectionID  string
	NoDeps        bool
	Prio          int
}

// ErrNotFlatpakrepo is returned when the data has no [Flatpak Repo] group.
var ErrNotFlatpakrepo = errors.New("not a .flatpakrepo file: missing [Flatpak Repo] group")

// ParseFlatpakrepo parses the INI-style .flatpakrepo format. Only the
// [Flatpak Repo] group is read; unknown keys are ignored.
func ParseFlatpakrepo(data []byte) (*RepoFile, error) {
	rf := &RepoFile{}
	inGroup, seenGroup := false, false
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inGroup = strings.TrimSpace(line[1:len(line)-1]) == "Flatpak Repo"
			if inGroup {
				seenGroup = true
			}
			continue
		}
		if !inGroup {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Title":
			rf.Title = value
		case "Url":
			rf.URL = value
		case "Homepage":
			rf.Homepage = value
		case "Comment":
			rf.Comment = value
		case "Description":
			rf.Description = value
		case "Icon":
			rf.Icon = value
		case "DefaultBranch":
			rf.DefaultBranch = value
		case "Filter":
			rf.Filter = value
		case "Subset":
			rf.Subset = value
		case "CollectionID", "DeployCollectionID":
			if rf.CollectionID == "" || key == "CollectionID" {
				rf.CollectionID = value
			}
		case "NoDeps":
			rf.NoDeps = strings.EqualFold(value, "true")
		case "Prio":
			if n, err := strconv.Atoi(value); err == nil {
				rf.Prio = n
			}
		case "GPGKey":
			key, err := decodeBase64Loose(value)
			if err != nil {
				return nil, fmt.Errorf("flatpakrepo: invalid GPGKey: %w", err)
			}
			rf.GPGKey = key
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("flatpakrepo: read: %w", err)
	}
	if !seenGroup {
		return nil, ErrNotFlatpakrepo
	}
	if rf.URL == "" {
		return nil, errors.New("flatpakrepo: missing Url")
	}
	return rf, nil
}

// decodeBase64Loose accepts standard base64 with or without padding and
// ignores embedded whitespace.
func decodeBase64Loose(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}
