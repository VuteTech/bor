// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/VuteTech/Bor/server/internal/ratelimit"
)

func TestRateLimitInterceptor(t *testing.T) {
	const enroll = "/bor.enrollment.v1.EnrollmentService/Enroll"
	const other = "/bor.enrollment.v1.EnrollmentService/RenewCertificate"
	ic := RateLimitInterceptor(map[string]bool{enroll: true}, ratelimit.New(2, time.Minute))
	handler := func(context.Context, any) (any, error) { return "ok", nil }

	ctxFor := func(ip string, port int) context.Context {
		return peer.NewContext(context.Background(), &peer.Peer{
			Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: port},
		})
	}
	call := func(ctx context.Context, method string) codes.Code {
		_, err := ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
		return status.Code(err)
	}

	// Different source ports from one host share a bucket.
	if c := call(ctxFor("198.51.100.7", 1000), enroll); c != codes.OK {
		t.Fatalf("call 1: %v", c)
	}
	if c := call(ctxFor("198.51.100.7", 1001), enroll); c != codes.OK {
		t.Fatalf("call 2: %v", c)
	}
	if c := call(ctxFor("198.51.100.7", 1002), enroll); c != codes.ResourceExhausted {
		t.Fatalf("call 3: got %v, want ResourceExhausted", c)
	}
	if c := call(ctxFor("198.51.100.8", 1000), enroll); c != codes.OK {
		t.Fatalf("other host: %v", c)
	}
	for i := 0; i < 5; i++ {
		if c := call(ctxFor("198.51.100.7", 1000), other); c != codes.OK {
			t.Fatalf("unlisted method throttled: %v", c)
		}
	}

	off := RateLimitInterceptor(map[string]bool{enroll: true}, nil)
	for i := 0; i < 5; i++ {
		if _, err := off(ctxFor("198.51.100.7", 1), nil, &grpc.UnaryServerInfo{FullMethod: enroll}, handler); err != nil {
			t.Fatalf("nil limiter rejected a call: %v", err)
		}
	}
}
