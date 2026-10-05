package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	manifest "github.com/Muxcore-Media/logging-file"
)

func TestEnvRotationConfig(t *testing.T) {
	t.Setenv("LOG_MAX_SIZE_MB", "7")
	t.Setenv("LOG_MAX_BACKUPS", "5")
	m := NewModule(Config{LogPath: filepath.Join(t.TempDir(), "env.log"), GRPCAddr: "127.0.0.1:0"})
	if m.maxSize != 7*1024*1024 {
		t.Fatalf("maxSize = %d want 7MiB", m.maxSize)
	}
	if m.maxBackups != 5 {
		t.Fatalf("maxBackups = %d want 5", m.maxBackups)
	}
}

func TestEnvInvalidMaxSizeWarnsAndKeepsDefault(t *testing.T) {
	t.Setenv("LOG_MAX_SIZE_MB", "nope")
	m := NewModule(Config{LogPath: filepath.Join(t.TempDir(), "bad.log"), GRPCAddr: "127.0.0.1:0"})
	if m.maxSize != 100*1024*1024 {
		t.Fatalf("maxSize = %d want default 100MiB", m.maxSize)
	}
}

func TestEnvStdoutTruthy(t *testing.T) {
	for _, v := range []string{"1", "yes", "on", "true"} {
		t.Setenv("LOG_STDOUT", v)
		m := NewModule(Config{LogPath: filepath.Join(t.TempDir(), "stdout.log"), GRPCAddr: "127.0.0.1:0"})
		if !m.stdout {
			t.Fatalf("LOG_STDOUT=%q should enable stdout", v)
		}
	}
}

func TestModuleInfo(t *testing.T) {
	m := NewModule(Config{})
	info := m.Info()
	if info.ID == "" {
		t.Error("module ID must not be empty")
	}
	if want := modulesdk.ManifestVersion(manifest.ManifestJSON); info.Version != want {
		t.Errorf("version = %q, want manifest version %q", info.Version, want)
	}
	if info.MinCoreVersion == "" {
		t.Error("MinCoreVersion must not be empty")
	}
	if len(info.Contracts) == 0 {
		t.Error("Contracts must not be empty")
	}
	if info.Contracts[0].Interface != "StructuredLogger" {
		t.Errorf("expected StructuredLogger contract, got %s", info.Contracts[0].Interface)
	}
	if len(info.Capabilities) == 0 {
		t.Error("Capabilities must not be empty")
	}
	if info.Capabilities[0] != "logging" {
		t.Errorf("expected logging capability, got %s", info.Capabilities[0])
	}
}

func TestStructuredLoggerInterface(t *testing.T) {
	m := NewModule(Config{LogPath: filepath.Join(t.TempDir(), "iface.log"), GRPCAddr: "127.0.0.1:0", SkipListen: true})
	log := m.Logger()
	var _ = log.(contracts.RedactingLogger)
	if !log.(contracts.RedactingLogger).IsRedacting() {
		t.Fatal("expected redacting logger")
	}
}

func newTestModule(t *testing.T) (*Module, string) {
	t.Helper()
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	m := NewModule(Config{
		LogPath:    logPath,
		GRPCAddr:   "127.0.0.1:0",
		Level:      "debug",
		SkipListen: true,
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Stop(ctx)
	})
	return m, logPath
}

func readLogLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer func() { _ = f.Close() }()

	var entries []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal: %v (line: %q)", err, string(line))
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestLogInfo(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	_, err := m.Log(ctx, &loggingv1.LogRequest{
		Level:        loggingv1.Level_LEVEL_INFO,
		Message:      "hello world",
		SourceModule: "test-module",
		Fields:       map[string]string{"key": "value"},
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := readLogLines(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0]["level"] != "info" {
		t.Errorf("expected level=info, got %v", entries[0]["level"])
	}
	if entries[0]["message"] != "hello world" {
		t.Errorf("expected message=hello world, got %v", entries[0]["message"])
	}
	if entries[0]["source"] != "test-module" {
		t.Errorf("expected source=test-module, got %v", entries[0]["source"])
	}
	if entries[0]["key"] != "value" {
		t.Errorf("expected key=value, got %v", entries[0]["key"])
	}
	if _, ok := entries[0]["timestamp"]; !ok {
		t.Error("expected timestamp field")
	}
}

func TestLogReadableWithoutStop(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()
	_, err := m.Log(ctx, &loggingv1.LogRequest{
		Level:   loggingv1.Level_LEVEL_INFO,
		Message: "flush test",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := readLogLines(t, logPath)
	if len(entries) != 1 || entries[0]["message"] != "flush test" {
		t.Fatalf("expected flushed entry on disk, got %#v", entries)
	}
}

func TestStructuredLoggerMethods(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()
	log := m.Logger()
	log.Debug(ctx, "dbg", map[string]any{"k": "v"})
	log.Info(ctx, "inf", nil)
	log.Warn(ctx, "wrn", nil)
	log.Error(ctx, "err", nil)
	entries := readLogLines(t, logPath)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
}

func TestLogAllLevels(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	levels := []loggingv1.Level{
		loggingv1.Level_LEVEL_DEBUG,
		loggingv1.Level_LEVEL_INFO,
		loggingv1.Level_LEVEL_WARN,
		loggingv1.Level_LEVEL_ERROR,
	}
	for _, l := range levels {
		_, err := m.Log(ctx, &loggingv1.LogRequest{
			Level:   l,
			Message: levelString(l),
		})
		if err != nil {
			t.Fatalf("Log %s: %v", levelString(l), err)
		}
	}

	entries := readLogLines(t, logPath)
	if len(entries) != 4 {
		t.Fatalf("expected 4 log entries, got %d", len(entries))
	}
	expected := []string{"debug", "info", "warn", "error"}
	for i, e := range entries {
		if e["level"] != expected[i] {
			t.Errorf("entry %d: expected level=%s, got %v", i, expected[i], e["level"])
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	m.level.Store(int32(loggingv1.Level_LEVEL_WARN))

	msgs := []struct {
		level loggingv1.Level
		msg   string
	}{
		{loggingv1.Level_LEVEL_DEBUG, "should-be-dropped"},
		{loggingv1.Level_LEVEL_INFO, "should-be-dropped"},
		{loggingv1.Level_LEVEL_WARN, "should-be-kept"},
		{loggingv1.Level_LEVEL_ERROR, "should-be-kept"},
	}
	for _, m2 := range msgs {
		_, _ = m.Log(ctx, &loggingv1.LogRequest{Level: m2.level, Message: m2.msg})
	}

	entries := readLogLines(t, logPath)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries (warn+error), got %d", len(entries))
	}
}

func TestSetLevel(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	resp, err := m.SetLevel(ctx, &loggingv1.SetLevelRequest{Level: loggingv1.Level_LEVEL_ERROR})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PreviousLevel != loggingv1.Level_LEVEL_DEBUG {
		t.Errorf("expected previous level debug, got %v", resp.PreviousLevel)
	}

	if loggingv1.Level(m.level.Load()) != loggingv1.Level_LEVEL_ERROR {
		t.Errorf("expected current level error, got %v", loggingv1.Level(m.level.Load()))
	}

	_, _ = m.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_INFO, Message: "dropped"})
	_, _ = m.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_ERROR, Message: "kept"})

	entries := readLogLines(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (only error kept), got %d", len(entries))
	}
	if entries[0]["message"] != "kept" {
		t.Errorf("expected message=kept, got %v", entries[0]["message"])
	}
}

func TestRedactSensitiveFields(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	_, err := m.Log(ctx, &loggingv1.LogRequest{
		Level:   loggingv1.Level_LEVEL_INFO,
		Message: "sensitive data",
		Fields: map[string]string{
			"password": "my-secret",
			"api_key":  "sk-1234",
			"username": "alice",
			"token":    "eyJhbGci",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := readLogLines(t, logPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0]["password"] != "***REDACTED***" {
		t.Errorf("expected password redacted, got %v", entries[0]["password"])
	}
	if entries[0]["api_key"] != "***REDACTED***" {
		t.Errorf("expected api_key redacted, got %v", entries[0]["api_key"])
	}
	if entries[0]["token"] != "***REDACTED***" {
		t.Errorf("expected token redacted, got %v", entries[0]["token"])
	}
	if entries[0]["username"] != "alice" {
		t.Errorf("expected username=alice, got %v", entries[0]["username"])
	}
}

func TestRotation(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rotating.log")
	m := NewModule(Config{
		LogPath:    logPath,
		GRPCAddr:   "127.0.0.1:0",
		Level:      "debug",
		MaxBackups: 2,
		SkipListen: true,
	})
	m.mu.Lock()
	m.maxSize = 256
	m.mu.Unlock()
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	for i := 0; i < 400; i++ {
		_, _ = m.Log(ctx, &loggingv1.LogRequest{
			Level:   loggingv1.Level_LEVEL_INFO,
			Message: strings.Repeat("x", 200),
		})
	}

	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Fatalf("expected rotated file .1: %v", err)
	}
	if _, err := os.Stat(logPath + ".2"); err != nil {
		t.Fatalf("expected rotated file .2: %v", err)
	}
	if _, err := os.Stat(logPath + ".3"); !os.IsNotExist(err) {
		t.Fatalf("expected no .3 backup beyond max_backups=2, err=%v", err)
	}
}

func TestHealth(t *testing.T) {
	m, _ := newTestModule(t)
	ctx := context.Background()
	if err := m.Health(ctx); err != nil {
		t.Fatalf("expected health to pass after start: %v", err)
	}
}

func TestHealthFailsAfterStop(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	logPath := filepath.Join(dir, "stop.log")
	m := NewModule(Config{LogPath: logPath, GRPCAddr: "127.0.0.1:0", SkipListen: true})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Health(ctx); err == nil {
		t.Fatal("expected health failure after stop")
	}
}

func TestModuleLifecycleTLS(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "")
	t.Setenv("MUXCORE_GRPC_INSECURE", "")

	dir := t.TempDir()
	logPath := filepath.Join(dir, "tls.log")
	m := NewModule(Config{
		LogPath:    logPath,
		GRPCAddr:   "127.0.0.1:0",
		SkipListen: true,
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestHealthFailsBeforeStart(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	m := NewModule(Config{LogPath: filepath.Join(dir, "h.log"), GRPCAddr: "127.0.0.1:0", SkipListen: true})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Health(ctx); err == nil {
		t.Fatal("expected health failure before start")
	}
}

func TestInitListenFailureClearsFile(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{LogPath: filepath.Join(dir, "fail.log"), GRPCAddr: "invalid://bad"})
	ctx := context.Background()
	if err := m.Init(ctx); err == nil {
		t.Fatal("expected listen failure")
	}
	m.mu.RLock()
	f := m.file
	m.mu.RUnlock()
	if f != nil {
		t.Fatal("expected file nil after init listen failure")
	}
}

func TestLifecycle(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	m := NewModule(Config{
		LogPath:    filepath.Join(t.TempDir(), "lifecycle.log"),
		GRPCAddr:   "127.0.0.1:0",
		SkipListen: true,
	})
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Health(ctx); err != nil {
		t.Fatal("expected health to pass after start")
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentLogging(t *testing.T) {
	m, logPath := newTestModule(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = m.Log(ctx, &loggingv1.LogRequest{
					Level:        loggingv1.Level_LEVEL_INFO,
					Message:      "concurrent",
					SourceModule: "test",
					Fields:       map[string]string{"goroutine": strings.Repeat("a", id%50+1)},
				})
			}
		}(i)
	}
	wg.Wait()

	entries := readLogLines(t, logPath)
	expected := 50 * 20
	if len(entries) != expected {
		t.Fatalf("expected %d log entries, got %d", expected, len(entries))
	}
}

func TestLevelString(t *testing.T) {
	cases := []struct {
		level loggingv1.Level
		want  string
	}{
		{loggingv1.Level_LEVEL_DEBUG, "debug"},
		{loggingv1.Level_LEVEL_INFO, "info"},
		{loggingv1.Level_LEVEL_WARN, "warn"},
		{loggingv1.Level_LEVEL_ERROR, "error"},
		{loggingv1.Level_LEVEL_UNSPECIFIED, "unknown"},
	}
	for _, c := range cases {
		got := levelString(c.level)
		if got != c.want {
			t.Errorf("levelString(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestLevelFromString(t *testing.T) {
	cases := []struct {
		s    string
		want loggingv1.Level
	}{
		{"debug", loggingv1.Level_LEVEL_DEBUG},
		{"DEBUG", loggingv1.Level_LEVEL_DEBUG},
		{"info", loggingv1.Level_LEVEL_INFO},
		{"warn", loggingv1.Level_LEVEL_WARN},
		{"error", loggingv1.Level_LEVEL_ERROR},
		{"unknown", loggingv1.Level_LEVEL_INFO},
	}
	for _, c := range cases {
		got := loggingv1.Level(levelFromString(c.s))
		if got != c.want {
			t.Errorf("levelFromString(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestSettingsLogPathAndLevel(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.log")
	pathB := filepath.Join(dir, "b.log")
	m := NewModule(Config{LogPath: pathA, GRPCAddr: "127.0.0.1:0", Level: "info", MaxSizeMB: 1, MaxBackups: 2, SkipListen: true})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defs := m.Settings()
	if len(defs) != 6 {
		t.Fatalf("Settings len=%d want 6", len(defs))
	}
	if err := m.UpdateSetting("level", "error"); err != nil {
		t.Fatal(err)
	}
	if loggingv1.Level(m.level.Load()) != loggingv1.Level_LEVEL_ERROR {
		t.Fatal("level not updated")
	}
	if err := m.UpdateSetting("log_path", pathB); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_ERROR, Message: "moved"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Fatalf("expected log at %s: %v", pathB, err)
	}
}

func TestSettingsStdoutMaxBackupsUnknown(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{LogPath: filepath.Join(dir, "s.log"), GRPCAddr: "127.0.0.1:0", SkipListen: true})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"yes", "on", "1"} {
		if err := m.UpdateSetting("stdout", v); err != nil {
			t.Fatalf("stdout %q: %v", v, err)
		}
	}
	m.mu.RLock()
	if !m.stdout {
		t.Fatal("stdout not enabled")
	}
	m.mu.RUnlock()
	if err := m.UpdateSetting("max_size_mb", "12"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateSetting("max_backups", "1"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateSetting("nope", "x"); err == nil {
		t.Fatal("expected unknown setting error")
	}
	_ = m.Stop(ctx)
}

func TestLogPathEscapeRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "logs")
	m := NewModule(Config{LogPath: filepath.Join(root, "app.log"), AllowedRoot: root, SkipListen: true})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside.log")
	if err := m.UpdateSetting("log_path", outside); err == nil {
		t.Fatal("expected rejection for path outside allowed root")
	}
	if err := m.UpdateSetting("log_path", filepath.Join(root, "..", "escape.log")); err == nil {
		t.Fatal("expected rejection for .. escape")
	}
	_ = m.Stop(ctx)
}

func TestGRPCAuthDeniedAndAllowed(t *testing.T) {
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	dir := t.TempDir()
	logPath := filepath.Join(dir, "auth.log")
	m := NewModule(Config{
		LogPath:     logPath,
		ModuleToken: "sekrit",
		SkipListen:  true,
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	m.SetListener(lis)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(ctx) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	cli := loggingv1.NewLogServiceClient(conn)

	_, err = cli.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_INFO, Message: "nope"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}

	mdCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-caller-id", "peer-mod"))
	_, err = cli.Log(mdCtx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_INFO, Message: "ok"})
	if err != nil {
		t.Fatalf("authed log: %v", err)
	}

	tokenCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer sekrit"))
	_, err = cli.SetLevel(tokenCtx, &loggingv1.SetLevelRequest{Level: loggingv1.Level_LEVEL_WARN})
	if err != nil {
		t.Fatalf("token set level: %v", err)
	}
}

func TestValidateLogPath(t *testing.T) {
	root := t.TempDir()
	if err := validateLogPath("", root); err == nil {
		t.Fatal("empty path")
	}
	if err := validateLogPath(filepath.Join(root, "..", "x.log"), root); err == nil {
		t.Fatal(".. path")
	}
	ok := filepath.Join(root, "ok.log")
	if err := validateLogPath(ok, root); err != nil {
		t.Fatalf("valid path: %v", err)
	}
}
