// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// UserGroupService handles user group business logic (identity domain)
type UserGroupService struct {
	repo *database.UserGroupRepository
}

// NewUserGroupService creates a new UserGroupService
func NewUserGroupService(repo *database.UserGroupRepository) *UserGroupService {
	return &UserGroupService{repo: repo}
}

// CreateUserGroup creates a new user group
func (s *UserGroupService) CreateUserGroup(ctx context.Context, req *models.CreateUserGroupRequest) (*models.UserGroup, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	ug := &models.UserGroup{
		Name:        req.Name,
		Description: req.Description,
	}
	if err := s.repo.Create(ctx, ug); err != nil {
		return nil, fmt.Errorf("failed to create user group: %w", err)
	}
	return ug, nil
}

// GetUserGroup retrieves a user group by ID
func (s *UserGroupService) GetUserGroup(ctx context.Context, id string) (*models.UserGroup, error) {
	return s.repo.GetByID(ctx, id)
}

// ListUserGroups returns all user groups
func (s *UserGroupService) ListUserGroups(ctx context.Context) ([]*models.UserGroup, error) {
	return s.repo.ListAll(ctx)
}

// UpdateUserGroup updates a user group
func (s *UserGroupService) UpdateUserGroup(ctx context.Context, id string, req *models.UpdateUserGroupRequest) (*models.UserGroup, error) {
	if err := s.repo.Update(ctx, id, req); err != nil {
		return nil, fmt.Errorf("failed to update user group: %w", err)
	}
	return s.repo.GetByID(ctx, id)
}

// DeleteUserGroup deletes a user group
func (s *UserGroupService) DeleteUserGroup(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
