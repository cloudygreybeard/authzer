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
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"  info ", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"", LevelInfo},
		{"bogus", LevelInfo},
	}
	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestLoggerEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, LevelInfo)

	l.Info("test.event", map[string]any{"key": "value"})

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}
	if entry.Level != "info" {
		t.Errorf("level = %q, want info", entry.Level)
	}
	if entry.Event != "test.event" {
		t.Errorf("event = %q, want test.event", entry.Event)
	}
	if entry.Timestamp == "" {
		t.Error("timestamp is empty")
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, LevelWarn)

	l.Debug("debug.event", nil)
	l.Info("info.event", nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for sub-threshold events, got %q", buf.String())
	}

	l.Warn("warn.event", nil)
	if buf.Len() == 0 {
		t.Error("expected output for warn event")
	}

	buf.Reset()
	l.Error("error.event", nil)
	if buf.Len() == 0 {
		t.Error("expected output for error event")
	}
}

func TestLoggerEnabled(t *testing.T) {
	l := NewLogger(nil, LevelWarn)

	if l.Enabled(LevelDebug) {
		t.Error("debug should not be enabled at warn level")
	}
	if l.Enabled(LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !l.Enabled(LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
	if !l.Enabled(LevelError) {
		t.Error("error should be enabled at warn level")
	}
}

func TestLoggerNilWriter(t *testing.T) {
	l := NewLogger(nil, LevelDebug)
	l.Debug("should.not.panic", nil)
	l.Info("should.not.panic", nil)
}

func TestLoggerMultipleEntries(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, LevelDebug)

	l.Debug("first", nil)
	l.Info("second", map[string]string{"k": "v"})
	l.Warn("third", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d: unmarshal: %v", i, err)
		}
	}
}
