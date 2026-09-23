// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"context"
	"fmt"
	"math"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// Wrappers for the disk-encryption RPCs. The recovery-key parameters travel as []byte so callers can zero
// them; they exist as strings only inside the request structs, for the
// duration of the call.

// ReportDiskEncryptionState publishes the inventory and returns pending tasks.
func (c *Client) ReportDiskEncryptionState(ctx context.Context, req *pb.ReportDiskEncryptionStateRequest) ([]*pb.DiskEncryptionTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req.ClientId = c.clientID
	resp, err := c.client.ReportDiskEncryptionState(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ReportDiskEncryptionState RPC failed: %w", err)
	}
	return resp.GetTasks(), nil
}

// GetDiskEncryptionTasks fetches the node's pending disk-encryption tasks.
func (c *Client) GetDiskEncryptionTasks(ctx context.Context) ([]*pb.DiskEncryptionTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := c.client.GetDiskEncryptionTasks(ctx, &pb.GetDiskEncryptionTasksRequest{ClientId: c.clientID})
	if err != nil {
		return nil, fmt.Errorf("GetDiskEncryptionTasks RPC failed: %w", err)
	}
	return resp.GetTasks(), nil
}

// EscrowRecoveryKey stores a new recovery key server-side BEFORE its keyslot
// exists (escrow-first two-phase commit). Implements luks.EscrowClient.
func (c *Client) EscrowRecoveryKey(ctx context.Context, luksUUID, escrowID string, recoveryKey []byte, reason pb.RecoveryKeyReason, rotationID string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req := &pb.EscrowRecoveryKeyRequest{
		ClientId:    c.clientID,
		LuksUuid:    luksUUID,
		EscrowId:    escrowID,
		RecoveryKey: string(recoveryKey),
		Reason:      reason,
		RotationId:  rotationID,
	}
	resp, err := c.client.EscrowRecoveryKey(ctx, req)
	req.RecoveryKey = ""
	if err != nil {
		return fmt.Errorf("EscrowRecoveryKey RPC failed: %w", err)
	}
	if !resp.GetAccepted() {
		return fmt.Errorf("server rejected the escrow for volume %s", luksUUID)
	}
	return nil
}

// ConfirmRecoveryKey promotes a pending escrow to ACTIVE once its keyslot
// exists and was verified. Implements luks.EscrowClient.
func (c *Client) ConfirmRecoveryKey(ctx context.Context, luksUUID, escrowID string, keyslot int, fingerprint string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := c.client.ConfirmRecoveryKey(ctx, &pb.ConfirmRecoveryKeyRequest{
		ClientId:           c.clientID,
		LuksUuid:           luksUUID,
		EscrowId:           escrowID,
		Keyslot:            clampUint32(keyslot),
		KeyslotFingerprint: fingerprint,
	})
	if err != nil {
		return fmt.Errorf("ConfirmRecoveryKey RPC failed: %w", err)
	}
	if !resp.GetAccepted() {
		return fmt.Errorf("server rejected the confirmation for escrow %s", escrowID)
	}
	return nil
}

// BeginRecoveryKeyRotation asks for the ACTIVE key as the rotation
// credential (granted only while a rotation task exists). Implements
// luks.EscrowClient. The caller must zero the returned key.
func (c *Client) BeginRecoveryKeyRotation(ctx context.Context, luksUUID string) (rotationID string, recoveryKey []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := c.client.BeginRecoveryKeyRotation(ctx, &pb.BeginRecoveryKeyRotationRequest{
		ClientId: c.clientID,
		LuksUuid: luksUUID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("BeginRecoveryKeyRotation RPC failed: %w", err)
	}
	key := []byte(resp.GetCurrentRecoveryKey())
	resp.CurrentRecoveryKey = ""
	return resp.GetRotationId(), key, nil
}

// clampUint32 converts an int to uint32, clamping at both bounds (LUKS2
// keyslot indexes are 0..31, so the clamps never fire in practice).
func clampUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if uint64(v) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}
