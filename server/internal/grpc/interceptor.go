// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// RevocationChecker is the interface used by the interceptors to check
// whether a client certificate serial has been revoked.
type RevocationChecker interface {
	IsRevoked(ctx context.Context, serial string) (bool, error)
}

// RequireClientCertInterceptor returns a unary server interceptor that
// requires a verified TLS client certificate for all methods except those
// in the exemptMethods set (e.g. the Enroll RPC which bootstraps mTLS).
// If rc is non-nil the cert serial is also checked against the revocation list.
func RequireClientCertInterceptor(exemptMethods map[string]bool, rc RevocationChecker) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if exemptMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		if err := verifyClientCert(ctx); err != nil {
			return nil, err
		}

		if rc != nil {
			if err := checkRevocation(ctx, rc); err != nil {
				return nil, err
			}
		}

		return handler(ctx, req)
	}
}

// RequireClientCertStreamInterceptor returns a stream server interceptor
// that requires a verified TLS client certificate for all streaming
// methods except those in the exemptMethods set.
// If rc is non-nil the cert serial is also checked against the revocation list.
func RequireClientCertStreamInterceptor(exemptMethods map[string]bool, rc RevocationChecker) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if exemptMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		if err := verifyClientCert(ss.Context()); err != nil {
			return err
		}

		if rc != nil {
			if err := checkRevocation(ss.Context(), rc); err != nil {
				return err
			}
		}

		return handler(srv, ss)
	}
}

// verifyClientCert checks that the context contains a verified TLS client certificate.
func verifyClientCert(ctx context.Context) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "no peer info")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "no TLS info – client certificate required")
	}

	if len(tlsInfo.State.VerifiedChains) == 0 {
		return status.Errorf(codes.Unauthenticated, "client certificate required – please enroll first")
	}

	return nil
}

// extractCertSerial extracts the serial number (as a lowercase hex string) from
// the verified TLS client certificate in the context.
func extractCertSerial(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Errorf(codes.Unauthenticated, "no peer info")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return "", status.Errorf(codes.Unauthenticated, "no verified client certificate")
	}
	cert := tlsInfo.State.VerifiedChains[0][0]
	return cert.SerialNumber.Text(16), nil
}

// checkRevocation returns a gRPC Unauthenticated error when the client
// certificate serial has been revoked.
func checkRevocation(ctx context.Context, rc RevocationChecker) error {
	serial, err := extractCertSerial(ctx)
	if err != nil {
		return err
	}
	revoked, err := rc.IsRevoked(ctx, serial)
	if err != nil {
		return status.Errorf(codes.Internal, "revocation check failed")
	}
	if revoked {
		return status.Errorf(codes.Unauthenticated, "certificate has been revoked")
	}
	return nil
}
