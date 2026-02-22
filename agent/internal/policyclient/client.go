// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policyclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Client wraps the gRPC PolicyService client.
type Client struct {
	conn     *grpc.ClientConn
	client   pb.PolicyServiceClient
	clientID string
}

// New creates a gRPC client connection to the given server address.
// caCertPath specifies the CA certificate to verify the server.
// clientCertPath and clientKeyPath specify the mTLS client certificate
// and private key. If insecureSkipVerify is true, server cert verification
// is skipped.
func New(serverAddr, clientID, caCertPath, clientCertPath, clientKeyPath string, insecureSkipVerify bool) (*Client, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if caCertPath != "" {
		caPEM, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate %s: %w", caCertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", caCertPath)
		}
		tlsCfg.RootCAs = pool
	} else if insecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
		log.Println("WARNING: server certificate verification is disabled (insecure_skip_verify)")
	} else {
		return nil, fmt.Errorf("no CA certificate configured and insecure_skip_verify is false – cannot connect securely; enroll first or set insecure_skip_verify")
	}

	// Load client certificate for mTLS if present
	if clientCertPath != "" && clientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server %s: %w", serverAddr, err)
	}

	return &Client{
		conn:     conn,
		client:   pb.NewPolicyServiceClient(conn),
		clientID: clientID,
	}, nil
}

// Close closes the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// PolicyInfo holds the policy data returned from the server.
type PolicyInfo struct {
	ID             string
	Name           string
	Type           string
	Content        string
	Version        int32
	KConfigEntries []*pb.KConfigEntry  // populated from typed_content for Kconfig type
	FirefoxPolicy  *pb.FirefoxPolicy   // populated from typed_content for Firefox type
}


// ReportCompliance sends a compliance report for a policy back to the server.
func (c *Client) ReportCompliance(ctx context.Context, policyID string, compliant bool, message string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.client.ReportCompliance(ctx, &pb.ReportComplianceRequest{
		ClientId:   c.clientID,
		PolicyId:   policyID,
		Compliant:  compliant,
		Message:    message,
		ReportedAt: timestamppb.Now(),
	})
	if err != nil {
		return fmt.Errorf("ReportCompliance RPC failed: %w", err)
	}

	if !resp.GetSuccess() {
		return fmt.Errorf("server rejected compliance report for policy %s", policyID)
	}

	return nil
}

// AgentConfig holds agent configuration retrieved from the server.
type AgentConfig struct {
	NotifyUsers          bool
	NotifyCooldown       int32
	NotifyMessage        string
	NotifyMessageFirefox string
	NotifyMessageChrome  string
}

// GetAgentConfig fetches agent configuration (notification settings)
// from the server.
func (c *Client) GetAgentConfig(ctx context.Context) (*AgentConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.client.GetAgentConfig(ctx, &pb.GetAgentConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("GetAgentConfig RPC failed: %w", err)
	}

	cfg := resp.GetConfig()
	if cfg == nil {
		return &AgentConfig{}, nil
	}

	return &AgentConfig{
		NotifyUsers:          cfg.GetNotifyUsers(),
		NotifyCooldown:       cfg.GetNotifyCooldownSeconds(),
		NotifyMessage:        cfg.GetNotifyMessage(),
		NotifyMessageFirefox: cfg.GetNotifyMessageFirefox(),
		NotifyMessageChrome:  cfg.GetNotifyMessageChrome(),
	}, nil
}

// NodeInfo holds system metadata sent with each heartbeat.
type NodeInfo struct {
	FQDN        string
	IPAddress   string
	OSName      string
	OSVersion   string
	DesktopEnvs []string
	AgentVersion string
	MachineID   string
}

// Heartbeat sends a heartbeat with node metadata to the server.
func (c *Client) Heartbeat(ctx context.Context, info *NodeInfo) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req := &pb.HeartbeatRequest{
		ClientId: c.clientID,
		Info: &pb.NodeInfo{
			Fqdn:         info.FQDN,
			IpAddress:    info.IPAddress,
			OsName:       info.OSName,
			OsVersion:    info.OSVersion,
			DesktopEnvs:  info.DesktopEnvs,
			AgentVersion: info.AgentVersion,
			MachineId:    info.MachineID,
		},
	}

	resp, err := c.client.Heartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("Heartbeat RPC failed: %w", err)
	}
	if !resp.GetAccepted() {
		return fmt.Errorf("server rejected heartbeat for client %s", c.clientID)
	}
	return nil
}

// PolicyUpdateCallback is called for each policy update received from
// the server streaming RPC. The revision parameter is the server-side
// revision after the update; the caller should persist this value to
// resume from on reconnect.
type PolicyUpdateCallback func(updateType string, policy *PolicyInfo, revision int64, snapshotComplete bool)

// SubscribePolicyUpdates opens a server-side streaming RPC and invokes
// cb for every policy update. It blocks until the context is cancelled
// or the stream errors out.
//
// lastKnownRevision should be 0 for first-time connect, or the last
// revision value received from the server on a previous session.
func (c *Client) SubscribePolicyUpdates(ctx context.Context, lastKnownRevision int64, cb PolicyUpdateCallback) error {
	stream, err := c.client.SubscribePolicyUpdates(ctx, &pb.SubscribePolicyUpdatesRequest{
		ClientId:          c.clientID,
		LastKnownRevision: lastKnownRevision,
	})
	if err != nil {
		return fmt.Errorf("SubscribePolicyUpdates RPC failed: %w", err)
	}

	for {
		update, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("stream recv error: %w", err)
		}

		var pi *PolicyInfo
		if p := update.GetPolicy(); p != nil {
			pi = &PolicyInfo{
				ID:      p.GetId(),
				Name:    p.GetName(),
				Type:    p.GetType(),
				Content: p.GetContent(),
				Version: p.GetVersion(),
			}
			if kcp := p.GetKconfigPolicy(); kcp != nil {
				pi.KConfigEntries = kcp.GetEntries()
			}
			if ffp := p.GetFirefoxPolicy(); ffp != nil {
				pi.FirefoxPolicy = ffp
			}
		}

		cb(update.GetType().String(), pi, update.GetRevision(), update.GetSnapshotComplete())
	}
}
