// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package reports builds compliance report bundles (pkg/report) from the
// server's compliance results and renders their built-in download formats.
// It has no database access: the service collects the source data and
// stores the result.
package reports

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/VuteTech/Bor/server/pkg/ext"
)

// Built-in download formats a renderer may not shadow.
const (
	FormatJSON = "json"
	FormatCSV  = "csv"
	FormatJWS  = "jws"
)

// Registry holds the signer and the document renderers extension modules
// registered. It implements ext.ReportRegistry.
type Registry struct {
	mu        sync.RWMutex
	signer    ext.ReportSigner
	renderers map[string]ext.ReportRenderer
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{renderers: make(map[string]ext.ReportRenderer)}
}

// RegisterSigner implements ext.ReportRegistry.
func (r *Registry) RegisterSigner(s ext.ReportSigner) error {
	if s == nil {
		return errors.New("reports: nil signer")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.signer != nil {
		return errors.New("reports: a signer is already registered")
	}
	r.signer = s
	return nil
}

// RegisterRenderer implements ext.ReportRegistry.
func (r *Registry) RegisterRenderer(x ext.ReportRenderer) error {
	if x == nil {
		return errors.New("reports: nil renderer")
	}
	format := x.Format()
	switch format {
	case "", FormatJSON, FormatCSV, FormatJWS:
		return fmt.Errorf("reports: renderer format %q is reserved", format)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.renderers[format]; dup {
		return fmt.Errorf("reports: renderer format %q is already registered", format)
	}
	r.renderers[format] = x
	return nil
}

// Signer returns the registered signer, or nil.
func (r *Registry) Signer() ext.ReportSigner {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.signer
}

// Renderer returns the renderer of a format, or nil.
func (r *Registry) Renderer(format string) ext.ReportRenderer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.renderers[format]
}

// Formats lists the registered renderer formats, sorted.
func (r *Registry) Formats() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.renderers))
	for f := range r.renderers {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
