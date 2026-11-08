package downloader

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"goto-bangumi/internal/apperrors"
)

// 下载器错误分类约定:
//   - 请求没能送达/完成(连接失败、超时、gRPC Unavailable)返回 NetworkError, 调用方可稍后重试
//   - 其他错误(状态码异常、gRPC 的 InvalidArgument/Unauthenticated 等)返回普通 error, 重试没有意义
//   - ctx 已取消时原样返回, 不是网络问题

// httpError 包装 HTTP 请求本身失败的错误
func httpError(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return err
	}
	return &apperrors.NetworkError{Err: fmt.Errorf("%s: %w", op, err)}
}

// rpcError 包装 gRPC 调用失败的错误, 只有 Unavailable 和 DeadlineExceeded 视为网络错误
func rpcError(ctx context.Context, op string, err error) error {
	if ctx.Err() == nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.DeadlineExceeded:
			return &apperrors.NetworkError{Err: fmt.Errorf("%s: %w", op, err)}
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
