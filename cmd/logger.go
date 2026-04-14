// Copyright 2026 cloudygreybeard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level controls the minimum severity of log entries emitted.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "debug",
	LevelInfo:  "info",
	LevelWarn:  "warn",
	LevelError: "error",
}

// ParseLevel converts a string to a Level. Returns LevelInfo for
// unrecognised values.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// LogEntry is a single structured log record written as one JSON line.
type LogEntry struct {
	Timestamp string `json:"ts"`
	Level     string `json:"level"`
	Event     string `json:"event"`
	Version   string `json:"version"`
	Data      any    `json:"data,omitempty"`
}

// Logger writes structured JSONL audit entries to a destination writer.
// It is safe for concurrent use.
type Logger struct {
	w     io.Writer
	mu    sync.Mutex
	level Level
	enc   *json.Encoder
}

// NewLogger creates a Logger that writes to w at the given minimum level.
// If w is nil, all output is discarded.
func NewLogger(w io.Writer, level Level) *Logger {
	if w == nil {
		w = io.Discard
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Logger{w: w, level: level, enc: enc}
}

// Log emits a structured entry if level >= the logger's minimum.
func (l *Logger) Log(level Level, event string, data any) {
	if level < l.level {
		return
	}
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     levelNames[level],
		Event:     event,
		Version:   Version,
		Data:      data,
	}
	l.mu.Lock()
	_ = l.enc.Encode(entry)
	l.mu.Unlock()
}

func (l *Logger) Debug(event string, data any) { l.Log(LevelDebug, event, data) }
func (l *Logger) Info(event string, data any)  { l.Log(LevelInfo, event, data) }
func (l *Logger) Warn(event string, data any)  { l.Log(LevelWarn, event, data) }
func (l *Logger) Error(event string, data any) { l.Log(LevelError, event, data) }

// Enabled returns true if events at the given level would be emitted.
func (l *Logger) Enabled(level Level) bool {
	return level >= l.level
}

// auditLog is the package-level structured logger, initialised during
// config loading. Commands use this directly.
var auditLog = NewLogger(nil, LevelInfo)

// quiet controls whether logHuman output is suppressed.
var quiet bool

// logHuman writes human-readable output to stderr unless --quiet is set.
func logHuman(format string, args ...any) {
	if quiet {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}
