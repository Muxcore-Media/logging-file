package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Muxcore-Media/core/pkg/contracts"
	loggingv1 "github.com/Muxcore-Media/core/proto/gen/muxcore/logging/v1"
)

func (m *Module) Settings() []contracts.SettingDef {
	return m.settingsDefs()
}

func (m *Module) UpdateSetting(key, value string) error {
	return m.updateSetting(key, value)
}

func (m *Module) settingsDefs() []contracts.SettingDef {
	m.mu.RLock()
	path := m.logPath
	stdout := m.stdout
	maxMB := cfgMaxSizeMB(m.maxSize)
	backups := m.maxBackups
	m.mu.RUnlock()
	level := levelString(loggingv1.Level(m.level.Load()))
	return []contracts.SettingDef{
		{
			Key:         "log_path",
			Label:       "Log File Path",
			Type:        contracts.SettingTypeString,
			Value:       path,
			Default:     "/var/lib/logging-file/module.log",
			Description: "JSONL log file path (LOG_FILE_PATH); updates reopen the file",
			Group:       "Output",
		},
		{
			Key:         "level",
			Label:       "Log Level",
			Type:        contracts.SettingTypeSelect,
			Value:       level,
			Default:     "info",
			Description: "Minimum level (LOG_LEVEL)",
			Group:       "Output",
			Options:     []string{"debug", "info", "warn", "error"},
		},
		{
			Key:         "stdout",
			Label:       "Also Log To Stdout",
			Type:        contracts.SettingTypeBool,
			Value:       strconv.FormatBool(stdout),
			Default:     "false",
			Description: "Mirror entries to stdout (LOG_STDOUT)",
			Group:       "Output",
		},
		{
			Key:         "max_size_mb",
			Label:       "Max Size (MB)",
			Type:        contracts.SettingTypeInt,
			Value:       strconv.FormatInt(maxMB, 10),
			Default:     "100",
			Description: "Rotate when file exceeds this size (LOG_MAX_SIZE_MB)",
			Group:       "Rotation",
		},
		{
			Key:         "max_backups",
			Label:       "Max Backups",
			Type:        contracts.SettingTypeInt,
			Value:       strconv.Itoa(backups),
			Default:     "3",
			Description: "Rotated backup count (LOG_MAX_BACKUPS)",
			Group:       "Rotation",
		},
	}
}

func (m *Module) updateSetting(key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "log_path", "LOG_FILE_PATH":
		if value == "" {
			return fmt.Errorf("log_path must not be empty")
		}
		return m.reopenLogPath(value)
	case "level", "LOG_LEVEL":
		switch strings.ToLower(value) {
		case "debug", "info", "warn", "error":
			m.level.Store(levelFromString(value))
			return nil
		default:
			return fmt.Errorf("invalid level %q (debug|info|warn|error)", value)
		}
	case "stdout", "LOG_STDOUT":
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			m.mu.Lock()
			m.stdout = true
			m.mu.Unlock()
		case "false", "0", "no", "off":
			m.mu.Lock()
			m.stdout = false
			m.mu.Unlock()
		default:
			return fmt.Errorf("invalid stdout %q (true/false)", value)
		}
		return nil
	case "max_size_mb", "LOG_MAX_SIZE_MB":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("invalid max_size_mb %q (integer >= 1)", value)
		}
		m.mu.Lock()
		m.maxSize = int64(n) * 1024 * 1024
		m.mu.Unlock()
		return nil
	case "max_backups", "LOG_MAX_BACKUPS":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return fmt.Errorf("invalid max_backups %q (integer >= 1)", value)
		}
		m.mu.Lock()
		m.maxBackups = n
		m.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
}

func (m *Module) reopenLogPath(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	info, _ := f.Stat()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf != nil {
		_ = m.buf.Flush()
	}
	if m.file != nil {
		_ = m.file.Close()
	}
	m.logPath = path
	m.file = f
	m.buf = bufio.NewWriterSize(f, 64*1024)
	m.enc = json.NewEncoder(m.buf)
	m.fileSize = info.Size()
	return nil
}
