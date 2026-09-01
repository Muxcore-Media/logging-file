package client

import (
	"context"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
)

// Client wraps LogService and implements StructuredLogger + RedactingLogger.
type Client struct {
	rpc          loggingv1.LogServiceClient
	sourceModule string
	level        atomic.Int32
}

// New returns a structured logger that sends entries to logging-file over gRPC.
func New(conn grpc.ClientConnInterface, sourceModule string) *Client {
	c := &Client{
		rpc:          loggingv1.NewLogServiceClient(conn),
		sourceModule: sourceModule,
	}
	c.level.Store(int32(loggingv1.Level_LEVEL_INFO))
	return c
}

func (c *Client) Debug(ctx context.Context, msg string, fields map[string]any) {
	c.log(ctx, loggingv1.Level_LEVEL_DEBUG, msg, fields)
}

func (c *Client) Info(ctx context.Context, msg string, fields map[string]any) {
	c.log(ctx, loggingv1.Level_LEVEL_INFO, msg, fields)
}

func (c *Client) Warn(ctx context.Context, msg string, fields map[string]any) {
	c.log(ctx, loggingv1.Level_LEVEL_WARN, msg, fields)
}

func (c *Client) Error(ctx context.Context, msg string, fields map[string]any) {
	c.log(ctx, loggingv1.Level_LEVEL_ERROR, msg, fields)
}

func (c *Client) Level() contracts.LogLevel {
	return protoLevelToContract(loggingv1.Level(c.level.Load()))
}

func (c *Client) IsRedacting() bool {
	return true
}

// SetLevel changes the remote minimum log level.
func (c *Client) SetLevel(ctx context.Context, level loggingv1.Level) (loggingv1.Level, error) {
	resp, err := c.rpc.SetLevel(ctx, &loggingv1.SetLevelRequest{Level: level})
	if err != nil {
		return loggingv1.Level_LEVEL_UNSPECIFIED, err
	}
	c.level.Store(int32(level))
	return resp.GetPreviousLevel(), nil
}

func (c *Client) log(ctx context.Context, level loggingv1.Level, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	strFields := make(map[string]string, len(fields))
	for k, v := range fields {
		strFields[k] = fmt.Sprint(v)
	}
	_, _ = c.rpc.Log(ctx, &loggingv1.LogRequest{
		Level:        level,
		Message:      msg,
		Fields:       strFields,
		SourceModule: c.sourceModule,
	})
}

func protoLevelToContract(l loggingv1.Level) contracts.LogLevel {
	switch l {
	case loggingv1.Level_LEVEL_DEBUG:
		return contracts.LogLevelDebug
	case loggingv1.Level_LEVEL_INFO:
		return contracts.LogLevelInfo
	case loggingv1.Level_LEVEL_WARN:
		return contracts.LogLevelWarn
	case loggingv1.Level_LEVEL_ERROR:
		return contracts.LogLevelError
	default:
		return contracts.LogLevelInfo
	}
}

var (
	_ contracts.StructuredLogger = (*Client)(nil)
	_ contracts.RedactingLogger  = (*Client)(nil)
)
