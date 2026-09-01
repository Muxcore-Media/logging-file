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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
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
	flushMs    int

	id          string
	logPath     string
	allowedRoot string
	grpcAddr    string
	moduleToken string
	skipListen  bool
	grpcSrv     *grpc.Server
	lis         net.Listener

	flushStop chan struct{}
	flushDone sync.WaitGroup
}

type Config struct {
	ID          string
	LogPath     string
	AllowedRoot string
	GRPCAddr    string
	Stdout      bool
	Level       string
	MaxSizeMB   int
	MaxBackups  int
	FlushMs     int
	ModuleToken string
	Version     string
	SkipListen  bool
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "logging-file"
	}
	if cfg.LogPath == "" {
		cfg.LogPath = "/var/lib/logging-file/module.log"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9625"
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
	if cfg.FlushMs == 0 {
		cfg.FlushMs = 250
	}
	if cfg.AllowedRoot == "" {
		cfg.AllowedRoot = filepath.Dir(cfg.LogPath)
	}
	if v := os.Getenv("LOG_FILE_PATH"); v != "" {
		cfg.LogPath = v
	}
	if v := os.Getenv("LOG_ALLOWED_ROOT"); v != "" {
		cfg.AllowedRoot = v
	} else if cfg.AllowedRoot == "" || cfg.AllowedRoot == filepath.Dir("/var/lib/logging-file/module.log") {
		cfg.AllowedRoot = filepath.Dir(cfg.LogPath)
	}
	if v := os.Getenv("LOG_GRPC_ADDR"); v != "" {
		cfg.GRPCAddr = v
	}
	if parseBoolEnv(os.Getenv("LOG_STDOUT")) {
		cfg.Stdout = true
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Level = v
	}
	if v := os.Getenv("LOG_MAX_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 {
			slog.Warn("invalid LOG_MAX_SIZE_MB, using default", "value", v, "default", cfg.MaxSizeMB)
		} else {
			cfg.MaxSizeMB = n
		}
	}
	if v := os.Getenv("LOG_MAX_BACKUPS"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 {
			slog.Warn("invalid LOG_MAX_BACKUPS, using default", "value", v, "default", cfg.MaxBackups)
		} else {
			cfg.MaxBackups = n
		}
	}
	if v := os.Getenv("LOG_FLUSH_MS"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 {
			slog.Warn("invalid LOG_FLUSH_MS, using default", "value", v, "default", cfg.FlushMs)
		} else {
			cfg.FlushMs = n
		}
	}
	if cfg.ModuleToken == "" {
		cfg.ModuleToken = moduleTokenFromEnv()
	}
	mod := &Module{
		id:          cfg.ID,
		logPath:     cfg.LogPath,
		allowedRoot: cfg.AllowedRoot,
		grpcAddr:    cfg.GRPCAddr,
		stdout:      cfg.Stdout,
		maxSize:     int64(cfg.MaxSizeMB) * 1024 * 1024,
		maxBackups:  cfg.MaxBackups,
		flushMs:     cfg.FlushMs,
		moduleToken: cfg.ModuleToken,
		skipListen:  cfg.SkipListen,
	}
	mod.level.Store(levelFromString(cfg.Level))
	return mod
}

func (m *Module) Info() contracts.ModuleInfo {
	ver := Version
	return contracts.ModuleInfo{
		ID:           m.id,
		Name:         "Logging File",
		Version:      ver,
		Roles:        []string{"infrastructure"},
		Description:  "File-backed structured logging provider with JSONL output, log rotation, and dynamic level control",
		Author:       "MuxCore",
		Capabilities: []string{contracts.CapabilityLogging, "logging.file", "settings"},
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
	if err := validateLogPath(m.logPath, m.allowedRoot); err != nil {
		return err
	}
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

	if !m.skipListen {
		lis, err := net.Listen("tcp", m.grpcAddr)
		if err != nil {
			_ = f.Close()
			m.mu.Lock()
			m.file = nil
			m.buf = nil
			m.enc = nil
			m.mu.Unlock()
			return fmt.Errorf("listen %s: %w", m.grpcAddr, err)
		}
		m.lis = lis
	}

	slog.Info("logging-file initialized",
		"path", m.logPath,
		"addr", m.grpcAddr,
		"level", m.level.Load(),
		"stdout", m.stdout,
		"max_size_mb", cfgMaxSizeMB(m.maxSize),
		"flush_ms", m.flushMs,
	)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if m.lis == nil {
		return errors.New("gRPC listener not configured")
	}
	m.grpcSrv = grpc.NewServer(grpc.UnaryInterceptor(authUnaryInterceptor(m.moduleToken)))
	loggingv1.RegisterLogServiceServer(m.grpcSrv, m)
	modulesdk.RegisterSettings(m.grpcSrv, m.id, m)

	m.flushStop = make(chan struct{})
	m.flushDone.Add(1)
	go m.flushLoop()

	go func(srv *grpc.Server, lis net.Listener) {
		slog.Info("logging-file gRPC service started", "addr", m.grpcAddr)
		if err := srv.Serve(lis); err != nil {
			slog.Error("logging-file gRPC serve error", "error", err)
		}
	}(m.grpcSrv, m.lis)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.flushStop != nil {
		close(m.flushStop)
		m.flushDone.Wait()
		m.flushStop = nil
	}
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
		m.grpcSrv = nil
	}
	m.lis = nil
	m.mu.Lock()
	if m.buf != nil {
		_ = m.buf.Flush()
	}
	if m.file != nil {
		_ = m.file.Close()
		m.file = nil
		m.buf = nil
		m.enc = nil
	}
	m.mu.Unlock()
	slog.Info("logging-file stopped")
	return nil
}

func (m *Module) Health(ctx context.Context) error {
	m.mu.RLock()
	f := m.file
	srv := m.grpcSrv
	lis := m.lis
	m.mu.RUnlock()
	if f == nil {
		return errors.New("log file not open")
	}
	if srv == nil || lis == nil {
		return errors.New("gRPC service not serving")
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("log file not writable: %w", err)
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
	stdout := m.stdout
	m.mu.RUnlock()

	m.mu.Lock()
	if err := m.writeWithRotation(entry); err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("write log: %w", err)
	}
	m.mu.Unlock()

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
	if err := m.buf.Flush(); err != nil {
		return err
	}
	if m.fileSize >= m.maxSize {
		return m.rotate()
	}
	return nil
}

func (m *Module) rotate() error {
	_ = m.buf.Flush()
	_ = m.file.Close()

	base := m.logPath
	for i := m.maxBackups - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", base, i)
		older := fmt.Sprintf("%s.%d", base, i+1)
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, older)
		}
	}
	if _, err := os.Stat(base); err == nil {
		_ = os.Rename(base, base+".1")
	}
	m.pruneRotatedFiles()

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

func (m *Module) pruneRotatedFiles() {
	base := m.logPath
	for i := m.maxBackups + 1; ; i++ {
		p := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		_ = os.Remove(p)
	}
}

func (m *Module) flushLoop() {
	defer m.flushDone.Done()
	if m.flushMs <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(m.flushMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.flushStop:
			return
		case <-ticker.C:
			m.mu.Lock()
			if m.buf != nil {
				_ = m.buf.Flush()
			}
			m.mu.Unlock()
		}
	}
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
	_, _ = os.Stdout.Write(data)
	_, _ = os.Stdout.Write([]byte{'\n'})
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

func contractsLogLevel(l loggingv1.Level) contracts.LogLevel {
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

func parseBoolEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
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

// SetListener attaches a net.Listener before Start (tests).
func (m *Module) SetListener(lis net.Listener) {
	m.lis = lis
}

// AuthUnaryInterceptor returns the LogService auth interceptor.
func AuthUnaryInterceptor(moduleToken string) grpc.UnaryServerInterceptor {
	return authUnaryInterceptor(moduleToken)
}

var _ contracts.Module = (*Module)(nil)
