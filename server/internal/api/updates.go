// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// updatesStore is the subset of database.UpdatesRepository the handler uses.
type updatesStore interface {
	GetNodeStatus(ctx context.Context, nodeID string) (*database.UpdatesNodeStatus, error)
	ListPending(ctx context.Context, nodeID, q string, securityOnly bool, limit int) ([]database.UpdatesPendingRow, error)
	FleetSummary(ctx context.Context, scope *models.GroupScopeFilter) (*models.UpdatesFleetSummary, error)
}

// updatesActionSender pushes an action to a connected agent (grpc.PolicyHub).
type updatesActionSender interface {
	SendUpdatesAction(clientID string, action *pb.UpdatesAction) bool
}

const (
	maxUpdatesQueryLength = 128
	// DefaultUpdatesActionTimeout is how long a check or reboot request
	// waits for the agent.
	DefaultUpdatesActionTimeout = 90 * time.Second
	// DefaultUpdatesInstallTimeout is how long an install request waits:
	// an engine run downloads and installs packages.
	DefaultUpdatesInstallTimeout = 15 * time.Minute
)

// UpdatesNodeHandler serves the Software updates state of one node and
// the fleet summary.
type UpdatesNodeHandler struct {
	nodes          nodeLookup
	store          updatesStore
	sender         updatesActionSender
	broker         *services.UpdatesActionBroker
	auditSvc       *services.AuditService
	anonymizeIPs   bool
	timeout        time.Duration
	installTimeout time.Duration
}

// NewUpdatesNodeHandler creates an UpdatesNodeHandler. sender and broker
// may be nil, in which case actions answer 503.
func NewUpdatesNodeHandler(nodes nodeLookup, store updatesStore, sender updatesActionSender, broker *services.UpdatesActionBroker, auditSvc *services.AuditService, anonymizeIPs bool) *UpdatesNodeHandler {
	return &UpdatesNodeHandler{nodes: nodes, store: store, sender: sender, broker: broker, auditSvc: auditSvc, anonymizeIPs: anonymizeIPs,
		timeout: DefaultUpdatesActionTimeout, installTimeout: DefaultUpdatesInstallTimeout}
}

func (h *UpdatesNodeHandler) loadNode(w http.ResponseWriter, r *http.Request, id string) (*models.Node, bool) {
	if id == "" || h.nodes == nil {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return nil, false
	}
	node, err := h.nodes.GetNode(r.Context(), id)
	if err != nil || node == nil || !requestScope(r, "node", "view").AllowsAny(node.NodeGroupIDs) {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
		return nil, false
	}
	return node, true
}

// updatesNodeResponse is GET /api/v1/nodes/{id}/updates.
type updatesNodeResponse struct {
	ReportedAt *time.Time      `json:"reportedAt,omitempty"`
	Inventory  json.RawMessage `json:"inventory,omitempty"`
}

// Get handles GET /api/v1/nodes/{id}/updates.
func (h *UpdatesNodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := h.store.GetNodeStatus(r.Context(), node.ID)
	if err != nil {
		log.Printf("Failed to load updates status of node %s: %v", node.ID, err)
		http.Error(w, `{"error":"failed to load status"}`, http.StatusInternalServerError)
		return
	}
	resp := updatesNodeResponse{}
	if st != nil {
		t := st.ReportedAt
		resp.ReportedAt = &t
		resp.Inventory = st.InfoJSON
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode updates node response: %v", err)
	}
}

// ListPending handles GET /api/v1/nodes/{id}/updates/pending?q=&security=1&limit=.
func (h *UpdatesNodeHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(q) > maxUpdatesQueryLength {
		q = q[:maxUpdatesQueryLength]
	}
	securityOnly := query.Get("security") == "1" || query.Get("security") == "true"
	limit, _ := strconv.Atoi(query.Get("limit"))
	rows, err := h.store.ListPending(r.Context(), node.ID, q, securityOnly, limit)
	if err != nil {
		log.Printf("Failed to list pending updates of node %s: %v", node.ID, err)
		http.Error(w, `{"error":"failed to list pending updates"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		log.Printf("Failed to encode pending updates: %v", err)
	}
}

// Summary handles GET /api/v1/updates/summary: the dashboard's counts of
// nodes with security updates pending, a reboot pending or no engine,
// within the caller's scope.
func (h *UpdatesNodeHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	sm, err := h.store.FleetSummary(r.Context(), requestScope(r, "node", "view").Filter())
	if err != nil {
		log.Printf("Failed to load the updates fleet summary: %v", err)
		http.Error(w, `{"error":"failed to load summary"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sm); err != nil {
		log.Printf("Failed to encode updates summary: %v", err)
	}
}

// updatesActionRequest is the body of POST /api/v1/nodes/{id}/updates/actions.
type updatesActionRequest struct {
	Kind  string `json:"kind"`
	Force bool   `json:"force"`
}

// updatesActionResponse is the answer once the agent reported back.
type updatesActionResponse struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Force   bool   `json:"force"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	Text    string `json:"text"`
}

var updatesActionKinds = map[string]pb.UpdatesActionKind{
	"check":   pb.UpdatesActionKind_UPDATES_ACTION_CHECK,
	"install": pb.UpdatesActionKind_UPDATES_ACTION_INSTALL,
	"reboot":  pb.UpdatesActionKind_UPDATES_ACTION_REBOOT,
}

// Action handles POST /api/v1/nodes/{id}/updates/actions: it asks the
// node's agent to refresh its metadata, run its update engine once or
// reboot, and waits for the outcome. Gated by updates:operate; confined
// to nodes whose every group is in the caller's scope.
func (h *UpdatesNodeHandler) Action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if !requestScope(r, "updates", "operate").AllowsAll(node.NodeGroupIDs) {
		denyOutOfScope(w, r, "updates", "operate", node.ID)
		return
	}
	var req updatesActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	kind, known := updatesActionKinds[req.Kind]
	if !known {
		writeJSONError(w, http.StatusBadRequest, "kind must be check, install or reboot")
		return
	}
	if req.Force && kind != pb.UpdatesActionKind_UPDATES_ACTION_REBOOT {
		req.Force = false
	}
	if h.sender == nil || h.broker == nil {
		http.Error(w, `{"error":"node actions are not available"}`, http.StatusServiceUnavailable)
		return
	}
	actionID, results, forget, err := h.broker.NewAction(node.Name)
	if err != nil {
		log.Printf("updates action: %v", err)
		http.Error(w, `{"error":"failed to create action"}`, http.StatusInternalServerError)
		return
	}
	defer forget()
	cmd := &pb.UpdatesAction{Id: actionID, Kind: kind, Force: req.Force}
	if !h.sender.SendUpdatesAction(node.Name, cmd) {
		h.audit(r, node, actionID, req, "not_connected", "agent not connected")
		http.Error(w, `{"error":"agent not connected"}`, http.StatusServiceUnavailable)
		return
	}
	wait := h.timeout
	if kind == pb.UpdatesActionKind_UPDATES_ACTION_INSTALL {
		wait = h.installTimeout
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case res := <-results:
		outcome := "failed"
		if res.Success {
			outcome = "success"
		}
		h.audit(r, node, actionID, req, outcome, res.Message)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(updatesActionResponse{ID: actionID, Kind: req.Kind, Force: req.Force, Success: res.Success, Message: res.Message, Text: res.Text}); err != nil {
			log.Printf("Failed to encode updates action response: %v", err)
		}
	case <-timer.C:
		h.audit(r, node, actionID, req, "timeout", "no answer from the agent")
		writeJSONError(w, http.StatusGatewayTimeout, fmt.Sprintf("the agent did not answer within %s; the node page shows the outcome once it reports", wait.Round(time.Second)))
	case <-r.Context().Done():
		h.audit(r, node, actionID, req, "abandoned", "request cancelled before the agent answered")
	}
}

// audit records one action request with its outcome.
func (h *UpdatesNodeHandler) audit(r *http.Request, node *models.Node, actionID string, req updatesActionRequest, outcome, message string) {
	if h.auditSvc == nil {
		return
	}
	username := ""
	var userID *string
	if claims := GetUserFromContext(r.Context()); claims != nil {
		username = claims.Username
		if claims.UserID != "" {
			id := claims.UserID
			userID = &id
		}
	}
	details, _ := json.Marshal(map[string]string{
		"node":      node.Name,
		"kind":      req.Kind,
		"force":     strconv.FormatBool(req.Force),
		"action_id": actionID,
		"outcome":   outcome,
		"message":   message,
	})
	h.auditSvc.LogEvent(r.Context(), &models.AuditLog{
		UserID:       userID,
		Username:     username,
		Action:       "updates.action",
		ResourceType: "node",
		ResourceID:   node.ID,
		Details:      string(details),
		IPAddress:    extractAuditIP(r, h.anonymizeIPs),
	})
}
