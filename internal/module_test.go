package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
)

func TestModuleInfo(t *testing.T) {
	m := NewModule(Config{})
	info := m.Info()
	if info.ID == "" {
		t.Error("module ID must not be empty")
	}
	if info.Version == "" {
		t.Error("module version must not be empty")
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

func newTestModule(t *testing.T) (*Module, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	m := NewModule(Config{
		LogPath:  logPath,
		GRPCAddr: ":0",
		Level:    "debug",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		m.Stop(ctx)
	})
	return m, logPath
}

func readLogLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

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

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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
		m.Log(ctx, &loggingv1.LogRequest{Level: m2.level, Message: m2.msg})
	}

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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

	m.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_INFO, Message: "dropped"})
	m.Log(ctx, &loggingv1.LogRequest{Level: loggingv1.Level_LEVEL_ERROR, Message: "kept"})

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rotating.log")
	m := NewModule(Config{
		LogPath:  logPath,
		GRPCAddr: ":0",
		Level:    "debug",
	})
	m.mu.Lock()
	m.maxSize = 256
	m.mu.Unlock()
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ctx)

	for i := 0; i < 200; i++ {
		m.Log(ctx, &loggingv1.LogRequest{
			Level:   loggingv1.Level_LEVEL_INFO,
			Message: strings.Repeat("x", 200),
		})
	}

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

	entries := readLogLines(t, logPath)
	if len(entries) == 0 {
		t.Fatal("expected entries in current log file")
	}

	rotatedPath := logPath + ".1"
	if _, err := os.Stat(rotatedPath); err != nil {
		t.Fatalf("expected rotated file %s: %v", rotatedPath, err)
	}
}

func TestHealth(t *testing.T) {
	m, _ := newTestModule(t)
	ctx := context.Background()
	if err := m.Health(ctx); err != nil {
		t.Fatal("expected health to pass after init")
	}
}

func TestLifecycle(t *testing.T) {
	m := NewModule(Config{
		LogPath:  filepath.Join(t.TempDir(), "lifecycle.log"),
		GRPCAddr: ":0",
	})
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
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
				m.Log(ctx, &loggingv1.LogRequest{
					Level:        loggingv1.Level_LEVEL_INFO,
					Message:      "concurrent",
					SourceModule: "test",
					Fields:       map[string]string{"goroutine": strings.Repeat("a", id%50+1)},
				})
			}
		}(i)
	}
	wg.Wait()

	m.mu.Lock()
	m.buf.Flush()
	m.mu.Unlock()

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
