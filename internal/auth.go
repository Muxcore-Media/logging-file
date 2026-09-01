package internal

import (
	"context"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const callerIDMetadataKey = "x-caller-id"

func moduleTokenFromEnv() string {
	for _, k := range []string{"LOG_MODULE_TOKEN", "MUXCORE_MODULE_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func authUnaryInterceptor(moduleToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !strings.HasPrefix(info.FullMethod, "/muxcore.logging.v1.LogService/") {
			return handler(ctx, req)
		}
		if err := authorizeLogRPC(ctx, moduleToken); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func authorizeLogRPC(ctx context.Context, moduleToken string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "mesh identity or module token required")
	}
	if ids := md.Get(callerIDMetadataKey); len(ids) > 0 {
		id := strings.TrimSpace(ids[0])
		if id != "" && id != "_public" {
			return nil
		}
	}
	if moduleToken != "" {
		if vals := md.Get("authorization"); len(vals) > 0 {
			token := bearerToken(vals[0])
			if token != "" && token == moduleToken {
				return nil
			}
		}
	}
	return status.Error(codes.Unauthenticated, "mesh identity or module token required")
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	header = strings.TrimSpace(header)
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return header
}
