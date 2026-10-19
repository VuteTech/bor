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
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"github.com/VuteTech/Bor/server/pkg/systemdunits"
)

// nodeLookup is the subset of services.NodeService the node systemd
// handler needs.
type nodeLookup interface {
	GetNode(ctx context.Context, id string) (*models.Node, error)
}

// systemdActionSender pushes a unit action to a connected agent
// (grpc.PolicyHub).
type systemdActionSender interface {
	SendSystemdUnitAction(clientID string, action *pb.SystemdUnitAction) bool
}

// DefaultSystemdActionTimeout is how long an action request waits for the
// agent's answer: a job timeout on the agent (90 s by default) plus slack.
const DefaultSystemdActionTimeout = 100 * time.Second

// SystemdNodeHandler serves the Services & timers section of the node
// details: the units a node reported, and start / stop / restart actions
// that run on the node at once and are audited.
type SystemdNodeHandler struct {
	nodes        nodeLookup
	units        systemdCatalogueStore
	sender       systemdActionSender
	broker       *services.SystemdActionBroker
	auditSvc     *services.AuditService
	anonymizeIPs bool
	timeout      time.Duration
}

// NewSystemdNodeHandler creates a SystemdNodeHandler. sender and broker may
// be nil, in which case actions answer 503.
func NewSystemdNodeHandler(nodes nodeLookup, units systemdCatalogueStore, sender systemdActionSender, broker *services.SystemdActionBroker, auditSvc *services.AuditService, anonymizeIPs bool) *SystemdNodeHandler {
	return &SystemdNodeHandler{
		nodes:        nodes,
		units:        units,
		sender:       sender,
		broker:       broker,
		auditSvc:     auditSvc,
		anonymizeIPs: anonymizeIPs,
		timeout:      DefaultSystemdActionTimeout,
	}
}

// loadNode fetches a node the caller may see (node:view in at least one
// of its groups); it answers 404 for a missing node and for one outside
// the caller's scope alike.
func (h *SystemdNodeHandler) loadNode(w http.ResponseWriter, r *http.Request, id string) (*models.Node, bool) {
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

// ListUnits handles GET /api/v1/nodes/{id}/systemd/units: the units the
// node reported, with the same query parameters as the catalogue
// endpoint (q, type).
func (h *SystemdNodeHandler) ListUnits(w http.ResponseWriter, r *http.Request) {
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
	if len(q) > maxSystemdQueryLength {
		q = q[:maxSystemdQueryLength]
	}
	typ := query.Get("type")
	if typ != "" && systemdunits.Type("x."+typ) == "" {
		http.Error(w, `{"error":"type must be one of service, timer, socket, target, path"}`, http.StatusBadRequest)
		return
	}
	units, err := h.units.ListUnitsByNode(r.Context(), node.ID, q, typ)
	if err != nil {
		log.Printf("Failed to list systemd units of node %s: %v", node.ID, err)
		http.Error(w, `{"error":"failed to list units"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(systemdUnitsResponse(units)); err != nil {
		log.Printf("Failed to encode node systemd units response: %v", err)
	}
}

// systemdActionRequest is the body of POST /api/v1/nodes/{id}/systemd/actions.
type systemdActionRequest struct {
	Unit   string `json:"unit"`
	Action string `json:"action"`
}

// systemdActionResponse is the answer once the agent reported back.
type systemdActionResponse struct {
	ID      string `json:"id"`
	Unit    string `json:"unit"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	// UnitState is the unit after the action, when the agent reported it.
	UnitState *systemdUnitResponse `json:"unitState,omitempty"`
}

var systemdActionKinds = map[string]pb.SystemdUnitActionKind{
	systemdunits.UnitActionStart:   pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_START,
	systemdunits.UnitActionStop:    pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_STOP,
	systemdunits.UnitActionRestart: pb.SystemdUnitActionKind_SYSTEMD_UNIT_ACTION_RESTART,
}

// Action handles POST /api/v1/nodes/{id}/systemd/actions: it sends a
// start, stop or restart of one unit to the node's agent over its policy
// stream and waits for the result. The route is gated by systemd:operate;
// the handler confines it to nodes whose every group is in the caller's
// scope. An action is never queued: a node that is not connected answers
// 503, and an agent that does not answer in time answers 504 (the action
// may still complete on the node; the unit list shows its state).
func (h *SystemdNodeHandler) Action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	node, ok := h.loadNode(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if !requestScope(r, "systemd", "operate").AllowsAll(node.NodeGroupIDs) {
		denyOutOfScope(w, r, "systemd", "operate", node.ID)
		return
	}
	var req systemdActionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.Unit = strings.TrimSpace(req.Unit)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if err := systemdunits.ValidateUnitAction(req.Unit, req.Action); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.sender == nil || h.broker == nil {
		http.Error(w, `{"error":"unit actions are not available"}`, http.StatusServiceUnavailable)
		return
	}

	actionID, results, forget, err := h.broker.NewAction(node.Name)
	if err != nil {
		log.Printf("systemd action: %v", err)
		http.Error(w, `{"error":"failed to create action"}`, http.StatusInternalServerError)
		return
	}
	defer forget()

	if !h.sender.SendSystemdUnitAction(node.Name, &pb.SystemdUnitAction{Id: actionID, Unit: req.Unit, Action: systemdActionKinds[req.Action]}) {
		h.audit(r, node, actionID, req, "not_connected", "agent not connected")
		http.Error(w, `{"error":"agent not connected"}`, http.StatusServiceUnavailable)
		return
	}

	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case res := <-results:
		outcome := "failed"
		if res.Success {
			outcome = "success"
		}
		h.audit(r, node, actionID, req, outcome, res.Message)
		resp := systemdActionResponse{ID: actionID, Unit: req.Unit, Action: req.Action, Success: res.Success, Message: res.Message}
		if res.Unit != nil {
			state := systemdUnitsResponse([]database.SystemdCatalogueUnit{{Info: res.Unit, NodeCount: 1, ReportedAt: time.Now()}})
			if len(state) == 1 {
				resp.UnitState = &state[0]
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Failed to encode systemd action response: %v", err)
		}
	case <-timer.C:
		h.audit(r, node, actionID, req, "timeout", "no answer from the agent")
		writeJSONError(w, http.StatusGatewayTimeout, fmt.Sprintf("the agent did not answer within %s; the action may still complete on the node", h.timeout.Round(time.Second)))
	case <-r.Context().Done():
		h.audit(r, node, actionID, req, "abandoned", "request cancelled before the agent answered")
	}
}

// audit records one unit action with its outcome. Every request that
// reached the agent (or could not, because it is offline) is recorded,
// because the action runs as root on the node.
func (h *SystemdNodeHandler) audit(r *http.Request, node *models.Node, actionID string, req systemdActionRequest, outcome, message string) {
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
		"unit":      req.Unit,
		"action":    req.Action,
		"action_id": actionID,
		"outcome":   outcome,
		"message":   message,
	})
	h.auditSvc.LogEvent(r.Context(), &models.AuditLog{
		UserID:       userID,
		Username:     username,
		Action:       "systemd.unit_action",
		ResourceType: "node",
		ResourceID:   node.ID,
		Details:      string(details),
		IPAddress:    extractAuditIP(r, h.anonymizeIPs),
	})
}
