// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/services"
)

// maxGroupMemberBatch bounds how many nodes one request may add to a group.
const maxGroupMemberBatch = 500

// NodeGroupMembersHandler changes group membership from the group side:
// add several nodes to a group in one call, or remove one member. The
// authorization rules are those of the per-node membership endpoints.
type NodeGroupMembersHandler struct {
	nodes        *NodeHandler
	nodeGroupSvc *services.NodeGroupService
}

// NewNodeGroupMembersHandler creates a NodeGroupMembersHandler. It shares
// the node handler's services and membership-change notification.
func NewNodeGroupMembersHandler(nodes *NodeHandler, nodeGroupSvc *services.NodeGroupService) *NodeGroupMembersHandler {
	return &NodeGroupMembersHandler{nodes: nodes, nodeGroupSvc: nodeGroupSvc}
}

// groupExists answers 404 itself when the group is missing.
func (h *NodeGroupMembersHandler) groupExists(w http.ResponseWriter, r *http.Request, groupID string) bool {
	group, err := h.nodeGroupSvc.GetNodeGroup(r.Context(), groupID)
	if err != nil || group == nil {
		writeJSONError(w, http.StatusNotFound, "node group not found")
		return false
	}
	return true
}

// Add handles POST /api/v1/node-groups/{id}/nodes with {"node_ids": [...]}.
// Every node is checked before anything is written, and the memberships are
// created in one statement, so the request applies fully or not at all.
func (h *NodeGroupMembersHandler) Add(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")

	var req struct {
		NodeIDs []string `json:"node_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	nodeIDs := uniqueNonEmpty(req.NodeIDs)
	if len(nodeIDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "node_ids is required")
		return
	}
	if len(nodeIDs) > maxGroupMemberBatch {
		writeJSONError(w, http.StatusBadRequest, "too many node_ids in one request")
		return
	}

	// The target group must be in scope: a delegated administrator can only
	// move nodes between their own groups.
	if !requestScope(r, "node", "create").Allows(groupID) {
		denyOutOfScope(w, r, "node_group", "create", groupID)
		return
	}
	if !h.groupExists(w, r, groupID) {
		return
	}
	for _, nodeID := range nodeIDs {
		if _, ok := h.nodes.loadWritableNode(w, r, nodeID, "create"); !ok {
			return
		}
	}

	added, err := h.nodes.nodeSvc.AddNodesToGroup(r.Context(), groupID, nodeIDs)
	if err != nil {
		log.Printf("Failed to add %d node(s) to group %s: %v", len(nodeIDs), groupID, err) //nolint:gosec // groupID comes from URL path parameter
		writeJSONError(w, http.StatusInternalServerError, "failed to add nodes to group")
		return
	}
	if h.nodes.OnMembershipChange != nil {
		for _, nodeID := range nodeIDs {
			h.nodes.OnMembershipChange(nodeID)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]int{"added": added}); err != nil {
		log.Printf("Failed to encode group members response: %v", err)
	}
}

// Remove handles DELETE /api/v1/node-groups/{id}/nodes/{node_id}.
func (h *NodeGroupMembersHandler) Remove(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	nodeID := r.PathValue("node_id")

	if _, ok := h.nodes.loadWritableNode(w, r, nodeID, "delete"); !ok {
		return
	}
	if !requestScope(r, "node", "delete").Allows(groupID) {
		denyOutOfScope(w, r, "node_group", "delete", groupID)
		return
	}
	if err := h.nodes.nodeSvc.RemoveNodeFromGroup(r.Context(), nodeID, groupID); err != nil {
		log.Printf("Failed to remove node %s from group %s: %v", nodeID, groupID, err) //nolint:gosec // ids come from URL path parameters
		writeJSONError(w, http.StatusInternalServerError, "failed to remove node from group")
		return
	}
	if h.nodes.OnMembershipChange != nil {
		h.nodes.OnMembershipChange(nodeID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// uniqueNonEmpty returns ids without blanks and duplicates, order kept.
func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
