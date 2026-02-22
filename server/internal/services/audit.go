// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// AuditService provides audit logging functionality
type AuditService struct {
	repo *database.AuditLogRepository
}

const exportBatchSize = 100

// NewAuditService creates a new AuditService
func NewAuditService(repo *database.AuditLogRepository) *AuditService {
	return &AuditService{repo: repo}
}

// LogEvent records an audit log entry
func (s *AuditService) LogEvent(ctx context.Context, entry *models.AuditLog) {
	if err := s.repo.Create(ctx, entry); err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}
}

// List retrieves audit logs with pagination
func (s *AuditService) List(ctx context.Context, req *models.AuditLogListRequest) (*models.AuditLogListResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage <= 0 {
		req.PerPage = 25
	}
	if req.PerPage > 100 {
		req.PerPage = 100
	}

	items, err := s.repo.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}

	total, err := s.repo.Count(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to count audit logs: %w", err)
	}

	if items == nil {
		items = []*models.AuditLog{}
	}

	totalPages := total / req.PerPage
	if total%req.PerPage > 0 {
		totalPages++
	}

	return &models.AuditLogListResponse{
		Items:      items,
		Total:      total,
		Page:       req.Page,
		PerPage:    req.PerPage,
		TotalPages: totalPages,
	}, nil
}

// ExportCSV writes audit logs as CSV to the given writer
func (s *AuditService) ExportCSV(ctx context.Context, req *models.AuditLogListRequest, w io.Writer) error {
	// Fetch all matching records (no pagination limit for exports)
	req.Page = 1
	req.PerPage = exportBatchSize

	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	// Write header
	if err := csvWriter.Write([]string{"ID", "Timestamp", "Username", "Action", "Resource Type", "Resource ID", "Details", "IP Address"}); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	for {
		items, err := s.repo.List(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list audit logs for export: %w", err)
		}

		for _, entry := range items {
			if err := csvWriter.Write([]string{
				entry.ID,
				entry.CreatedAt.Format("2006-01-02T15:04:05Z"),
				entry.Username,
				entry.Action,
				entry.ResourceType,
				entry.ResourceID,
				entry.Details,
				entry.IPAddress,
			}); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		}

		if len(items) < req.PerPage {
			break
		}
		req.Page++
	}

	return nil
}

// ExportJSON writes audit logs as JSON array to the given writer
func (s *AuditService) ExportJSON(ctx context.Context, req *models.AuditLogListRequest, w io.Writer) error {
	req.Page = 1
	req.PerPage = exportBatchSize

	var allItems []*models.AuditLog

	for {
		items, err := s.repo.List(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list audit logs for export: %w", err)
		}

		allItems = append(allItems, items...)

		if len(items) < req.PerPage {
			break
		}
		req.Page++
	}

	if allItems == nil {
		allItems = []*models.AuditLog{}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(allItems); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return nil
}
