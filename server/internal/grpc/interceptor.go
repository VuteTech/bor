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

// RequireClientCertInterceptor returns a unary server interceptor that
// requires a verified TLS client certificate for all methods except those
// in the exemptMethods set (e.g. the Enroll RPC which bootstraps mTLS).
func RequireClientCertInterceptor(exemptMethods map[string]bool) grpc.UnaryServerInterceptor {
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

		return handler(ctx, req)
	}
}

// RequireClientCertStreamInterceptor returns a stream server interceptor
// that requires a verified TLS client certificate for all streaming
// methods except those in the exemptMethods set.
func RequireClientCertStreamInterceptor(exemptMethods map[string]bool) grpc.StreamServerInterceptor {
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
