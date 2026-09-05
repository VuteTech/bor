// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package flatpakcatalog downloads and parses the AppStream catalogs of
// Flatpak remotes registered on the server and keeps the searchable copy in
// PostgreSQL current (docs/flatpak-policy-plan.md §6).
package flatpakcatalog

import (
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// maxDescriptionRunes caps the stored description (tags stripped).
const maxDescriptionRunes = 4096

// ErrCatalogTooLarge is returned when the decompressed catalog exceeds the
// configured cap (zip-bomb guard).
var ErrCatalogTooLarge = errors.New("appstream catalog exceeds the configured size limit")

type xmlLocalized struct {
	Lang  string `xml:"lang,attr"`
	Value string `xml:",chardata"`
}

type xmlDescription struct {
	Lang     string `xml:"lang,attr"`
	InnerXML string `xml:",innerxml"`
}

type xmlTypedValue struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type xmlBundle struct {
	Type    string `xml:"type,attr"`
	Runtime string `xml:"runtime,attr"`
	SDK     string `xml:"sdk,attr"`
	Value   string `xml:",chardata"`
}

type xmlRelease struct {
	Version   string `xml:"version,attr"`
	Timestamp string `xml:"timestamp,attr"`
	Date      string `xml:"date,attr"`
}

type xmlKeyedValue struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

type xmlDeveloper struct {
	ID    string         `xml:"id,attr"`
	Names []xmlLocalized `xml:"name"`
}

type xmlComponent struct {
	Type           string           `xml:"type,attr"`
	ID             string           `xml:"id"`
	Names          []xmlLocalized   `xml:"name"`
	Summaries      []xmlLocalized   `xml:"summary"`
	Descriptions   []xmlDescription `xml:"description"`
	DeveloperNames []xmlLocalized   `xml:"developer_name"`
	Developer      xmlDeveloper     `xml:"developer"`
	ProjectLicense string           `xml:"project_license"`
	URLs           []xmlTypedValue  `xml:"url"`
	Categories     []string         `xml:"categories>category"`
	Keywords       []xmlLocalized   `xml:"keywords>keyword"`
	Bundles        []xmlBundle      `xml:"bundle"`
	Releases       []xmlRelease     `xml:"releases>release"`
	Icons          []xmlTypedValue  `xml:"icon"`
	ContentRating  struct {
		Type string `xml:"type,attr"`
	} `xml:"content_rating"`
	Custom   []xmlKeyedValue `xml:"custom>value"`
	Metadata []xmlKeyedValue `xml:"metadata>value"`
}

// countingReader tracks bytes read so the decompressed size can be capped.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// ParseAppStreamGzip decompresses and parses a gzip'd AppStream catalog.
// maxDecompressed bounds the amount of XML read (0 = 512 MiB).
func ParseAppStreamGzip(r io.Reader, maxDecompressed int64) ([]models.FlatpakCatalogEntry, error) {
	if maxDecompressed <= 0 {
		maxDecompressed = 512 << 20
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("appstream: open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	counted := &countingReader{r: io.LimitReader(gz, maxDecompressed+1)}
	entries, err := ParseAppStream(counted)
	if counted.n > maxDecompressed {
		return nil, ErrCatalogTooLarge
	}
	return entries, err
}

// ParseAppStream parses an uncompressed AppStream catalog XML document. It
// streams component by component so memory stays proportional to a single
// component rather than the whole file.
func ParseAppStream(r io.Reader) ([]models.FlatpakCatalogEntry, error) {
	dec := xml.NewDecoder(r)
	var out []models.FlatpakCatalogEntry
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("appstream: parse: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "component" {
			continue
		}
		var c xmlComponent
		if err := dec.DecodeElement(&c, &se); err != nil {
			return nil, fmt.Errorf("appstream: decode component: %w", err)
		}
		if e, ok := convertComponent(&c); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// convertComponent maps one AppStream component to a catalog entry. It
// returns false for components without a Flatpak bundle reference.
func convertComponent(c *xmlComponent) (models.FlatpakCatalogEntry, bool) {
	var bundle *xmlBundle
	for i := range c.Bundles {
		if c.Bundles[i].Type == "flatpak" {
			bundle = &c.Bundles[i]
			break
		}
	}
	if bundle == nil {
		return models.FlatpakCatalogEntry{}, false
	}
	refKind, appID, arch, branch, ok := splitRef(strings.TrimSpace(bundle.Value))
	if !ok {
		return models.FlatpakCatalogEntry{}, false
	}

	e := models.FlatpakCatalogEntry{
		AppID:          appID,
		Arch:           arch,
		Branch:         branch,
		Ref:            strings.TrimSpace(bundle.Value),
		Kind:           componentKind(c.Type, refKind),
		Name:           untranslated(c.Names),
		Summary:        untranslated(c.Summaries),
		ProjectLicense: strings.TrimSpace(c.ProjectLicense),
		Runtime:        strings.TrimSpace(bundle.Runtime),
		ContentRating:  strings.TrimSpace(c.ContentRating.Type),
	}
	if e.Name == "" {
		e.Name = appID
	}
	for _, d := range c.Descriptions {
		if d.Lang == "" {
			e.Description = stripTags(d.InnerXML)
			break
		}
	}
	e.Developer = untranslated(c.Developer.Names)
	if e.Developer == "" {
		e.Developer = untranslated(c.DeveloperNames)
	}
	if e.Developer == "" {
		e.Developer = strings.TrimSpace(c.Developer.ID)
	}
	for _, u := range c.URLs {
		if u.Type == "homepage" {
			e.Homepage = strings.TrimSpace(u.Value)
			break
		}
	}
	for _, cat := range c.Categories {
		if cat = strings.TrimSpace(cat); cat != "" {
			e.Categories = append(e.Categories, cat)
		}
	}
	for _, k := range c.Keywords {
		if k.Lang == "" {
			if v := strings.TrimSpace(k.Value); v != "" {
				e.Keywords = append(e.Keywords, v)
			}
		}
	}
	if len(c.Releases) > 0 {
		rel := c.Releases[0]
		e.LatestVersion = strings.TrimSpace(rel.Version)
		e.LatestReleaseAt = releaseTime(rel)
	}
	e.IconFile = cachedIcon(c.Icons, appID)
	for _, kv := range append(append([]xmlKeyedValue{}, c.Custom...), c.Metadata...) {
		if kv.Key == "flathub::verification::verified" && strings.TrimSpace(kv.Value) == "true" {
			e.Verified = true
		}
	}
	return e, true
}

// splitRef parses "app/org.x.y/x86_64/stable".
func splitRef(ref string) (kind, id, arch, branch string, ok bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 4 || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return "", "", "", "", false
	}
	if parts[0] != "app" && parts[0] != "runtime" {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], parts[3], true
}

func componentKind(componentType, refKind string) string {
	switch componentType {
	case "desktop-application", "desktop":
		return "desktop-application"
	case "console-application":
		return "console-application"
	case "addon":
		return "addon"
	case "runtime":
		return "runtime"
	}
	if refKind == "runtime" {
		return "runtime"
	}
	return "other"
}

func untranslated(values []xmlLocalized) string {
	for _, v := range values {
		if v.Lang == "" {
			return strings.TrimSpace(v.Value)
		}
	}
	return ""
}

// cachedIcon picks the file name of a cached icon. Flathub stores them as
// icons/<size>/<file>; when only stock/remote icons are listed the catalog
// convention is <app-id>.png.
func cachedIcon(icons []xmlTypedValue, appID string) string {
	for _, ic := range icons {
		if ic.Type == "cached" {
			if v := strings.TrimSpace(ic.Value); v != "" && !strings.ContainsAny(v, "/\\") {
				return v
			}
		}
	}
	if len(icons) > 0 {
		return appID + ".png"
	}
	return ""
}

func releaseTime(rel xmlRelease) *time.Time {
	if ts := strings.TrimSpace(rel.Timestamp); ts != "" {
		if n, err := strconv.ParseInt(ts, 10, 64); err == nil && n > 0 {
			t := time.Unix(n, 0).UTC()
			return &t
		}
	}
	if d := strings.TrimSpace(rel.Date); d != "" {
		for _, layout := range []string{"2006-01-02", time.RFC3339} {
			if t, err := time.Parse(layout, d); err == nil {
				t = t.UTC()
				return &t
			}
		}
	}
	return nil
}

var (
	tagRE        = regexp.MustCompile(`<[^>]*>`)
	whitespaceRE = regexp.MustCompile(`\s+`)
)

// stripTags turns the XHTML-ish AppStream description into plain text.
func stripTags(s string) string {
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(whitespaceRE.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > maxDescriptionRunes {
		s = string(r[:maxDescriptionRunes-1]) + "…"
	}
	return s
}
