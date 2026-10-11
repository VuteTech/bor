// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package exthost

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/lib/pq"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	"github.com/VuteTech/Bor/server/pkg/ext"
)

func (i *identity) LinkFederatedIdentity(ctx context.Context, userID, provider, subject string) error {
	u, err := i.federatedUser(ctx, userID)
	if err != nil {
		return err
	}
	if provider == "" || subject == "" {
		return errors.New("ext: provider and subject are required")
	}
	if err := i.deps.Identities.Link(ctx, provider, subject, u.ID); err != nil {
		return err
	}
	i.auditChange(ctx, "user.link_identity", "users", u.ID, map[string]string{"username": u.Username, "provider": provider})
	return nil
}

func (i *identity) RenameFederatedUser(ctx context.Context, userID, username string) error {
	username = strings.TrimSpace(username)
	u, err := i.federatedUser(ctx, userID)
	if err != nil {
		return err
	}
	if username == "" || username == u.Username {
		return nil
	}
	if err := i.deps.Users.UpdateUsername(ctx, u.ID, username); err != nil {
		if isUniqueViolation(err) {
			return ext.ErrUsernameTaken
		}
		return err
	}
	i.auditChange(ctx, "user.update", "users", u.ID, map[string]string{"username": username, "previous_username": u.Username})
	return nil
}

func (i *identity) GetUser(ctx context.Context, userID string) (*ext.User, error) {
	u, err := i.deps.Users.GetByID(ctx, userID)
	if err != nil || u == nil {
		return nil, err
	}
	return toExt(u), nil
}

func (i *identity) DeleteFederatedUser(ctx context.Context, userID string) error {
	u, err := i.federatedUser(ctx, userID)
	if err != nil {
		return err
	}
	if err := i.deps.Auth.InvalidateUserTokens(ctx, u.ID); err != nil {
		return err
	}
	if err := i.deps.Auth.DeleteUser(ctx, u.ID); err != nil {
		if errors.Is(err, services.ErrLastSuperAdmin) {
			return ext.ErrLastSuperAdmin
		}
		return err
	}
	i.auditChange(ctx, "user.delete", "users", u.ID, map[string]string{"username": u.Username})
	return nil
}

// managedBy is how the host stores a module's group owner.
func (i *identity) managedBy(owner string) string { return i.module + ":" + owner }

// ownedGroup returns the group when owner manages it.
func (i *identity) ownedGroup(ctx context.Context, owner, groupID string) (*models.UserGroup, error) {
	if owner == "" {
		return nil, ext.ErrNotOwner
	}
	g, err := i.deps.Groups.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g == nil || g.ManagedBy == nil || *g.ManagedBy != i.managedBy(owner) {
		return nil, ext.ErrNotOwner
	}
	return g, nil
}

func toExtGroup(g *models.UserGroup) *ext.Group {
	out := &ext.Group{ID: g.ID, Name: g.Name, Description: g.Description}
	if g.ManagedBy != nil {
		out.ManagedBy = *g.ManagedBy
	}
	return out
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (i *identity) CreateManagedGroup(ctx context.Context, owner, name string) (*ext.Group, error) {
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return nil, errors.New("ext: owner and name are required")
	}
	managedBy := i.managedBy(owner)
	g := &models.UserGroup{Name: name, Description: "Managed by " + managedBy, ManagedBy: &managedBy}
	if err := i.deps.Groups.Create(ctx, g); err != nil {
		if isUniqueViolation(err) {
			return nil, ext.ErrGroupNameTaken
		}
		return nil, err
	}
	i.auditChange(ctx, "user_group.create", "user-groups", g.ID, map[string]string{"name": name, "managed_by": managedBy})
	return toExtGroup(g), nil
}

func (i *identity) RenameManagedGroup(ctx context.Context, owner, groupID, name string) error {
	name = strings.TrimSpace(name)
	g, err := i.ownedGroup(ctx, owner, groupID)
	if err != nil {
		return err
	}
	if name == "" || name == g.Name {
		return nil
	}
	if err := i.deps.Groups.Update(ctx, groupID, &models.UpdateUserGroupRequest{Name: &name}); err != nil {
		if isUniqueViolation(err) {
			return ext.ErrGroupNameTaken
		}
		return err
	}
	i.auditChange(ctx, "user_group.update", "user-groups", groupID, map[string]string{"name": name, "previous_name": g.Name})
	return nil
}

func (i *identity) DeleteManagedGroup(ctx context.Context, owner, groupID string) error {
	g, err := i.ownedGroup(ctx, owner, groupID)
	if err != nil {
		return err
	}
	if err := i.deps.Groups.Delete(ctx, groupID); err != nil {
		return err
	}
	i.auditChange(ctx, "user_group.delete", "user-groups", groupID, map[string]string{"name": g.Name})
	return nil
}

func (i *identity) ManagedGroups(ctx context.Context, owner string) ([]*ext.Group, error) {
	all, err := i.deps.Groups.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	managedBy := i.managedBy(owner)
	var out []*ext.Group
	for _, g := range all {
		if g.ManagedBy != nil && *g.ManagedBy == managedBy {
			out = append(out, toExtGroup(g))
		}
	}
	return out, nil
}

func (i *identity) ManagedGroupMembers(ctx context.Context, owner, groupID string) ([]string, error) {
	if _, err := i.ownedGroup(ctx, owner, groupID); err != nil {
		return nil, err
	}
	members, err := i.deps.Members.ListByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	sort.Strings(ids)
	return ids, nil
}

func (i *identity) UpdateManagedGroupMembers(ctx context.Context, owner, groupID string, add, remove []string) error {
	if _, err := i.ownedGroup(ctx, owner, groupID); err != nil {
		return err
	}
	members, err := i.deps.Members.ListByGroupID(ctx, groupID)
	if err != nil {
		return err
	}
	memberIDByUser := make(map[string]string, len(members))
	for _, m := range members {
		memberIDByUser[m.UserID] = m.ID
	}
	for _, userID := range add {
		if _, present := memberIDByUser[userID]; present {
			continue
		}
		m := &models.UserGroupMember{GroupID: groupID, UserID: userID}
		if err := i.deps.Members.Create(ctx, m); err != nil {
			return err
		}
		memberIDByUser[userID] = m.ID
		i.auditChange(ctx, "user_group.member_add", "user-groups", groupID, map[string]string{"user_id": userID})
	}
	for _, userID := range remove {
		memberID, present := memberIDByUser[userID]
		if !present {
			continue
		}
		if err := i.deps.Members.Delete(ctx, memberID); err != nil {
			return err
		}
		delete(memberIDByUser, userID)
		i.auditChange(ctx, "user_group.member_remove", "user-groups", groupID, map[string]string{"user_id": userID})
	}
	return nil
}
