// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/VuteTech/Bor/server/internal/ratelimit"
)

// RateLimitInterceptor limits the given unary methods per client IP address
// (the TCP peer, which a client cannot choose). Calls over the limit fail
// with ResourceExhausted. Methods not listed pass through. A nil limiter
// disables the check.
func RateLimitInterceptor(methods map[string]bool, l *ratelimit.Limiter) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if l == nil || !methods[info.FullMethod] {
			return handler(ctx, req)
		}
		ip := peerHost(ctx)
		if !l.Allow(ip) {
			log.Printf("rate limit: rejected %s from %s", info.FullMethod, ip)
			return nil, status.Errorf(codes.ResourceExhausted,
				"too many requests, retry in %s", l.Window())
		}
		return handler(ctx, req)
	}
}

// peerHost returns the IP of the gRPC peer without its port, or "unknown"
// when the context carries no peer.
func peerHost(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return "unknown"
	}
	addr := p.Addr.String()
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
