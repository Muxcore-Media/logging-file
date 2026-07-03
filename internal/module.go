package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
)

var sensitivePrefixes = contracts.SensitiveLogFieldNames()

type Module struct {
	loggingv1.UnimplementedLogServiceServer

	mu     sync.RWMutex
	file   *os.File
	buf    *bufio.Writer
	enc    *json.Encoder
	stdout bool

	level      atomic.Int32
	fileSize   int64
	maxSize    int64
	maxBackups int

	id       string
	logPath  string
	grpcAddr string
	grpcSrv  *grpc.Server
	lis      net.Listener
}

type Config struct {
	ID         string
	LogPath    string
	GRPCAddr   string
	Stdout     bool
	Level      string
	MaxSizeMB  int
	MaxBackups int
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "logging-file"
	}
	if cfg.LogPath == "" {
		cfg.LogPath = "/var/lib/logging-file/module.log"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9620"
	}
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.MaxSizeMB == 0 {
		cfg.MaxSizeMB = 100
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 3
	}
	if v := os.Getenv("LOG_FILE_PATH"); v != "" {
		cfg.LogPath = v
	}
	if v := os.Getenv("LOG_GRPC_ADDR"); v != "" {
		cfg.GRPCAddr = v
	}
	if v := os.Getenv("LOG_STDOUT"); v == "true" {
		cfg.Stdout = true
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Level = v
	}
	mod := &Module{
		id:         cfg.ID,
		logPath:    cfg.LogPath,
		grpcAddr:   cfg.GRPCAddr,
		stdout:     cfg.Stdout,
		maxSize:    int64(cfg.MaxSizeMB) * 1024 * 1024,
		maxBackups: cfg.MaxBackups,
	}
	mod.level.Store(levelFromString(cfg.Level))
	return mod
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:           m.id,
		Name:         "Logging File",
		Version:      "0.1.0",
		Roles:        []string{"infrastructure"},
		Description:  "File-backed structured logging provider with JSONL output, log rotation, and dynamic level control",
		Author:       "MuxCore",
		Capabilities: []string{contracts.CapabilityLogging},
		Contracts: []contracts.ContractDeclaration{
			{
				Repo:      "github.com/Muxcore-Media/core/pkg/contracts",
				Interface: "StructuredLogger",
				Version:   "v0.4.0",
			},
		},
		MinCoreVersion: "0.4.0",
		HTTPAddr:       m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	dir := filepath.Dir(m.logPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", m.logPath, err)
	}

	info, _ := f.Stat()
	m.mu.Lock()
	m.file = f
	m.buf = bufio.NewWriterSize(f, 64*1024)
	m.enc = json.NewEncoder(m.buf)
	m.fileSize = info.Size()
	m.mu.Unlock()

	lis, err := net.Listen("tcp", m.grpcAddr)
	if err != nil {
		f.Close()
		return fmt.Errorf("listen %s: %w", m.grpcAddr, err)
	}
	m.lis = lis

	slog.Info("logging-file initialized",
		"path", m.logPath,
		"addr", m.grpcAddr,
		"level", m.level.Load(),
		"stdout", m.stdout,
		"max_size_mb", cfgMaxSizeMB(m.maxSize),
	)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.grpcSrv = grpc.NewServer()
	loggingv1.RegisterLogServiceServer(m.grpcSrv, m)

	go func() {
		slog.Info("logging-file gRPC service started", "addr", m.grpcAddr)
		if err := m.grpcSrv.Serve(m.lis); err != nil {
			slog.Error("logging-file gRPC serve error", "error", err)
		}
	}()
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	m.mu.Lock()
	if m.buf != nil {
		m.buf.Flush()
	}
	if m.file != nil {
		m.file.Close()
		m.file = nil
	}
	m.mu.Unlock()
	slog.Info("logging-file stopped")
	return nil
}

func (m *Module) Health(ctx context.Context) error {
	m.mu.RLock()
	f := m.file
	m.mu.RUnlock()
	if f == nil {
		return errors.New("log file not open")
	}
	return nil
}

func (m *Module) Log(ctx context.Context, req *loggingv1.LogRequest) (*loggingv1.LogResponse, error) {
	level := req.GetLevel()
	if !shouldLog(level, loggingv1.Level(m.level.Load())) {
		return &loggingv1.LogResponse{}, nil
	}

	entry := newLogEntry(level, req.GetMessage(), req.GetFields(), req.GetSourceModule())

	m.mu.RLock()
	enc := m.enc
	stdout := m.stdout
	m.mu.RUnlock()

	if enc != nil {
		m.mu.Lock()
		if err := m.writeWithRotation(entry); err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("write log: %w", err)
		}
		m.mu.Unlock()
	}

	if stdout {
		writeStdout(level, entry)
	}

	return &loggingv1.LogResponse{}, nil
}

func (m *Module) SetLevel(ctx context.Context, req *loggingv1.SetLevelRequest) (*loggingv1.SetLevelResponse, error) {
	prev := m.level.Swap(int32(req.GetLevel()))
	return &loggingv1.SetLevelResponse{
		PreviousLevel: loggingv1.Level(prev),
	}, nil
}

func (m *Module) writeWithRotation(entry map[string]any) error {
	if m.fileSize >= m.maxSize {
		if err := m.rotate(); err != nil {
			return err
		}
	}

	if err := m.enc.Encode(entry); err != nil {
		return err
	}
	m.fileSize += estimateSize(entry)
	if m.fileSize >= m.maxSize {
		m.buf.Flush()
	}
	return nil
}

func (m *Module) rotate() error {
	m.buf.Flush()
	m.file.Close()

	base := m.logPath
	for i := m.maxBackups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", base, i)
		older := fmt.Sprintf("%s.%d", base, i+1)
		if _, err := os.Stat(old); err == nil {
			os.Rename(old, older)
		}
	}
	if _, err := os.Stat(base); err == nil {
		os.Rename(base, base+".1")
	}

	f, err := os.OpenFile(base, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("create new log file: %w", err)
	}

	m.file = f
	m.buf = bufio.NewWriterSize(f, 64*1024)
	m.enc = json.NewEncoder(m.buf)
	m.fileSize = 0
	return nil
}

func newLogEntry(level loggingv1.Level, msg string, fields map[string]string, source string) map[string]any {
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     levelString(level),
		"message":   msg,
	}
	if source != "" {
		entry["source"] = source
	}
	for k, v := range fields {
		if isSensitive(k) {
			entry[k] = "***REDACTED***"
		} else {
			entry[k] = v
		}
	}
	return entry
}

func writeStdout(level loggingv1.Level, entry map[string]any) {
	data, _ := json.Marshal(entry)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte{'\n'})
}

func shouldLog(msgLevel, minLevel loggingv1.Level) bool {
	return msgLevel >= minLevel
}

func levelString(l loggingv1.Level) string {
	switch l {
	case loggingv1.Level_LEVEL_DEBUG:
		return "debug"
	case loggingv1.Level_LEVEL_INFO:
		return "info"
	case loggingv1.Level_LEVEL_WARN:
		return "warn"
	case loggingv1.Level_LEVEL_ERROR:
		return "error"
	default:
		return "unknown"
	}
}

func levelFromString(s string) int32 {
	switch strings.ToLower(s) {
	case "debug":
		return int32(loggingv1.Level_LEVEL_DEBUG)
	case "info":
		return int32(loggingv1.Level_LEVEL_INFO)
	case "warn":
		return int32(loggingv1.Level_LEVEL_WARN)
	case "error":
		return int32(loggingv1.Level_LEVEL_ERROR)
	default:
		return int32(loggingv1.Level_LEVEL_INFO)
	}
}

func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, prefix := range sensitivePrefixes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

func estimateSize(entry map[string]any) int64 {
	data, _ := json.Marshal(entry)
	return int64(len(data)) + 1
}

func cfgMaxSizeMB(bytes int64) int64 {
	return bytes / (1024 * 1024)
}

var _ contracts.Module = (*Module)(nil)
