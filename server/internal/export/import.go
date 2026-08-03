// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
)

// Store interfaces — implemented by the policy/binding/node-group services.

// PolicyStore is the slice of PolicyService the importer needs.
type PolicyStore interface {
	ListAllPolicies(ctx context.Context) ([]*models.Policy, error)
	CreatePolicy(ctx context.Context, req *models.CreatePolicyRequest, createdBy string) (*models.Policy, error)
	UpdatePolicy(ctx context.Context, id string, req *models.UpdatePolicyRequest) (*models.Policy, error)
}

// BindingStore is the slice of PolicyBindingService the importer needs.
type BindingStore interface {
	CreateBinding(ctx context.Context, req *models.CreatePolicyBindingRequest) (*models.PolicyBinding, error)
}

// GroupStore is the slice of NodeGroupService the importer needs.
type GroupStore interface {
	ListNodeGroups(ctx context.Context) ([]*models.NodeGroup, error)
}

// Conflict handling for policies whose display name already exists.
const (
	OnConflictError      = "error"
	OnConflictSkip       = "skip"
	OnConflictNewVersion = "new-version"
)

// Options controls import behavior.
type Options struct {
	DryRun     bool
	OnConflict string // OnConflict* constants; default OnConflictError
	Actor      string // username recorded as created_by
}

// DocResult reports the outcome for one document in the bundle.
type DocResult struct {
	Doc     int    `json:"doc"` // 1-based document index
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Status  string `json:"status"` // created | updated | skipped | error
	Message string `json:"message,omitempty"`
}

// Report is the structured result of an import (or dry run).
type Report struct {
	Ok      bool        `json:"ok"`
	DryRun  bool        `json:"dry_run"`
	Created int         `json:"created"`
	Updated int         `json:"updated"`
	Skipped int         `json:"skipped"`
	Errors  int         `json:"errors"`
	Results []DocResult `json:"results"`
}

// ContentValidator is injected by the API layer to run the per-type service
// validators (services.Validate*Content) without an import cycle.
type ContentValidator func(policyType, content string) error

type pendingPolicy struct {
	doc      int
	slug     string
	name     string
	desc     string
	ptype    string
	content  string
	existing *models.Policy // non-nil when OnConflict resolves to update
	skip     bool
}

type pendingBinding struct {
	doc        int
	name       string
	policyRef  string
	groupID    string
	priority   int
	policySlug bool // ref resolved against an in-file slug
	policyID   string
}

// Import validates the whole bundle first and creates resources only when
// every document is valid ("validate all, then execute"): nothing is written
// for a bundle containing any invalid document. Execution-stage failures
// (e.g. concurrent deletes) are reported per document.
func Import(ctx context.Context, policies PolicyStore, bindings BindingStore, groups GroupStore, validate ContentValidator, data []byte, opts Options) (*Report, error) {
	if opts.OnConflict == "" {
		opts.OnConflict = OnConflictError
	}
	switch opts.OnConflict {
	case OnConflictError, OnConflictSkip, OnConflictNewVersion:
	default:
		return nil, fmt.Errorf("invalid on_conflict %q", opts.OnConflict)
	}

	report := &Report{DryRun: opts.DryRun}
	fail := func(doc int, kind, name, msg string) {
		report.Errors++
		report.Results = append(report.Results, DocResult{Doc: doc, Kind: kind, Name: name, Status: "error", Message: msg})
	}

	docs, err := SplitAndConvert(data)
	if err != nil {
		return nil, err
	}

	existing, err := policies.ListAllPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	byName := make(map[string]*models.Policy, len(existing))
	for _, p := range existing {
		byName[p.Name] = p
	}
	groupList, err := groups.ListNodeGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list node groups: %w", err)
	}
	groupByName := make(map[string]string, len(groupList))
	for _, g := range groupList {
		groupByName[g.Name] = g.ID
	}

	// ── Pass 1: parse + validate every document ──
	var pendingPolicies []*pendingPolicy
	var pendingBindings []*pendingBinding
	slugs := make(map[string]*pendingPolicy)
	namesInFile := make(map[string]bool)

	for i, doc := range docs {
		n := i + 1
		res, err := ParseResource(doc)
		if err != nil {
			fail(n, "", "", err.Error())
			continue
		}
		switch res.Kind {
		case "Policy":
			spec := res.GetPolicy()
			if spec == nil {
				fail(n, res.Kind, "", "missing spec")
				continue
			}
			name := strings.TrimSpace(res.GetMetadata().GetDisplayName())
			if name == "" {
				name = strings.TrimSpace(res.GetMetadata().GetName())
			}
			if name == "" {
				fail(n, res.Kind, "", "metadata.displayName (or name) is required")
				continue
			}
			if namesInFile[name] {
				fail(n, res.Kind, name, "duplicate policy name in bundle")
				continue
			}
			namesInFile[name] = true
			raw, err := ContentJSON(spec)
			if err != nil {
				fail(n, res.Kind, name, err.Error())
				continue
			}
			// Canonicalize (schema check + casing normalization), then run
			// the same per-type validator the create API uses.
			canon, err := CanonicalizeContent(spec.GetType(), raw)
			if err != nil {
				fail(n, res.Kind, name, err.Error())
				continue
			}
			if validate != nil {
				if err := validate(spec.GetType(), canon); err != nil {
					fail(n, res.Kind, name, err.Error())
					continue
				}
			}
			p := &pendingPolicy{
				doc:     n,
				slug:    res.GetMetadata().GetName(),
				name:    name,
				desc:    res.GetMetadata().GetDescription(),
				ptype:   spec.GetType(),
				content: canon,
			}
			if prev, ok := byName[name]; ok {
				switch opts.OnConflict {
				case OnConflictError:
					fail(n, res.Kind, name, "a policy with this name already exists (on_conflict=error)")
					continue
				case OnConflictSkip:
					p.skip = true
				case OnConflictNewVersion:
					if prev.State != "draft" {
						fail(n, res.Kind, name, fmt.Sprintf("existing policy is %s; only drafts can be updated (release it manually after import)", prev.State))
						continue
					}
					p.existing = prev
				}
			}
			if p.slug != "" {
				if _, dup := slugs[p.slug]; dup {
					fail(n, res.Kind, name, fmt.Sprintf("duplicate metadata.name %q in bundle", p.slug))
					continue
				}
				slugs[p.slug] = p
			}
			pendingPolicies = append(pendingPolicies, p)

		case "PolicyBinding":
			spec := res.GetBinding()
			if spec == nil {
				fail(n, res.Kind, "", "missing spec")
				continue
			}
			name := res.GetMetadata().GetName()
			if spec.GetPolicy() == "" || spec.GetGroup() == "" {
				fail(n, res.Kind, name, "spec.policy and spec.group are required")
				continue
			}
			b := &pendingBinding{
				doc:       n,
				name:      name,
				policyRef: spec.GetPolicy(),
				priority:  int(spec.GetPriority()),
			}
			gid, ok := groupByName[spec.GetGroup()]
			if !ok {
				fail(n, res.Kind, name, fmt.Sprintf("node group %q does not exist on this server", spec.GetGroup()))
				continue
			}
			b.groupID = gid
			if _, ok := slugs[b.policyRef]; ok {
				b.policySlug = true
			} else if prev, ok := byName[b.policyRef]; ok {
				b.policyID = prev.ID
			} else {
				fail(n, res.Kind, name, fmt.Sprintf("policy %q not found in bundle or on this server", b.policyRef))
				continue
			}
			pendingBindings = append(pendingBindings, b)
		}
	}

	// A bundle with any invalid document is rejected wholesale.
	if report.Errors > 0 {
		report.Ok = false
		return report, nil
	}

	// ── Dry run: report what would happen ──
	if opts.DryRun {
		for _, p := range pendingPolicies {
			switch {
			case p.skip:
				report.Skipped++
				report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "skipped", Message: "name exists"})
			case p.existing != nil:
				report.Updated++
				report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "updated", Message: "would update existing draft"})
			default:
				report.Created++
				report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "created", Message: "would create as draft"})
			}
		}
		for _, b := range pendingBindings {
			report.Created++
			report.Results = append(report.Results, DocResult{Doc: b.doc, Kind: "PolicyBinding", Name: b.name, Status: "created", Message: "would create"})
		}
		report.Ok = true
		return report, nil
	}

	// ── Pass 2: execute ──
	createdBy := opts.Actor
	slugToID := make(map[string]string)
	skippedNames := make(map[string]bool)
	for _, p := range pendingPolicies {
		switch {
		case p.skip:
			report.Skipped++
			skippedNames[p.name] = true
			report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "skipped", Message: "name exists"})
			if p.slug != "" {
				slugToID[p.slug] = byName[p.name].ID // bindings may still attach
			}
		case p.existing != nil:
			if _, err := policies.UpdatePolicy(ctx, p.existing.ID, &models.UpdatePolicyRequest{
				Description: &p.desc,
				Content:     &p.content,
			}); err != nil {
				fail(p.doc, "Policy", p.name, err.Error())
				continue
			}
			report.Updated++
			if p.slug != "" {
				slugToID[p.slug] = p.existing.ID
			}
			report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "updated"})
		default:
			created, err := policies.CreatePolicy(ctx, &models.CreatePolicyRequest{
				Name:        p.name,
				Description: p.desc,
				Type:        p.ptype,
				Content:     p.content,
			}, createdBy)
			if err != nil {
				fail(p.doc, "Policy", p.name, err.Error())
				continue
			}
			report.Created++
			if p.slug != "" {
				slugToID[p.slug] = created.ID
			}
			report.Results = append(report.Results, DocResult{Doc: p.doc, Kind: "Policy", Name: p.name, Status: "created"})
		}
	}
	for _, b := range pendingBindings {
		id := b.policyID
		if b.policySlug {
			id = slugToID[b.policyRef]
		}
		if id == "" {
			fail(b.doc, "PolicyBinding", b.name, fmt.Sprintf("policy %q was not imported", b.policyRef))
			continue
		}
		if _, err := bindings.CreateBinding(ctx, &models.CreatePolicyBindingRequest{
			PolicyID: id,
			GroupID:  b.groupID,
			Priority: b.priority,
		}); err != nil {
			fail(b.doc, "PolicyBinding", b.name, err.Error())
			continue
		}
		report.Created++
		report.Results = append(report.Results, DocResult{Doc: b.doc, Kind: "PolicyBinding", Name: b.name, Status: "created"})
	}

	report.Ok = report.Errors == 0
	return report, nil
}
