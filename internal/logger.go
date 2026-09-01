package internal

import (
	"context"
	"fmt"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
)

type moduleLogger struct {
	m *Module
}

// Logger returns a StructuredLogger view of the module (in-process logging).
func (m *Module) Logger() contracts.StructuredLogger {
	return &moduleLogger{m: m}
}

func (l *moduleLogger) Debug(ctx context.Context, msg string, fields map[string]any) {
	l.log(ctx, loggingv1.Level_LEVEL_DEBUG, msg, fields)
}

func (l *moduleLogger) Info(ctx context.Context, msg string, fields map[string]any) {
	l.log(ctx, loggingv1.Level_LEVEL_INFO, msg, fields)
}

func (l *moduleLogger) Warn(ctx context.Context, msg string, fields map[string]any) {
	l.log(ctx, loggingv1.Level_LEVEL_WARN, msg, fields)
}

func (l *moduleLogger) Error(ctx context.Context, msg string, fields map[string]any) {
	l.log(ctx, loggingv1.Level_LEVEL_ERROR, msg, fields)
}

func (l *moduleLogger) Level() contracts.LogLevel {
	return contractsLogLevel(loggingv1.Level(l.m.level.Load()))
}

func (l *moduleLogger) IsRedacting() bool {
	return true
}

func (l *moduleLogger) log(ctx context.Context, level loggingv1.Level, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	_, _ = l.m.Log(ctx, &loggingv1.LogRequest{
		Level:        level,
		Message:      msg,
		Fields:       stringifyFields(fields),
		SourceModule: l.m.id,
	})
}

func stringifyFields(fields map[string]any) map[string]string {
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = fmt.Sprint(v)
	}
	return out
}

var (
	_ contracts.StructuredLogger = (*moduleLogger)(nil)
	_ contracts.RedactingLogger  = (*moduleLogger)(nil)
)
