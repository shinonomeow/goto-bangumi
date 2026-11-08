package downloader

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"goto-bangumi/internal/apperrors"
)

func TestRPCErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		network bool
	}{
		{"Unavailable", status.Error(codes.Unavailable, "down"), true},
		{"DeadlineExceeded", status.Error(codes.DeadlineExceeded, "slow"), true},
		{"Unauthenticated", status.Error(codes.Unauthenticated, "bad token"), false},
		{"InvalidArgument", status.Error(codes.InvalidArgument, "bad path"), false},
		{"PermissionDenied", status.Error(codes.PermissionDenied, "denied"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rpcError(context.Background(), "op", tt.err)
			if apperrors.IsNetworkError(got) != tt.network {
				t.Fatalf("IsNetworkError = %v, want %v (err=%v)", !tt.network, tt.network, got)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("original error not wrapped: %v", got)
			}
		})
	}
}

func TestErrorsNotNetworkWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if apperrors.IsNetworkError(httpError(ctx, "op", context.Canceled)) {
		t.Fatal("httpError should not classify a canceled ctx as network error")
	}
	if apperrors.IsNetworkError(rpcError(ctx, "op", status.Error(codes.Unavailable, "x"))) {
		t.Fatal("rpcError should not classify a canceled ctx as network error")
	}
}

func TestHTTPErrorIsNetworkError(t *testing.T) {
	if !apperrors.IsNetworkError(httpError(context.Background(), "op", errors.New("connection refused"))) {
		t.Fatal("httpError should return NetworkError")
	}
}
