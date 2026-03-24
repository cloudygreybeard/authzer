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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestResolveScript_Inline(t *testing.T) {
	viper.Reset()
	viper.Set("test.js", "console.log('hello')")

	got, err := resolveScript("test.js")
	if err != nil {
		t.Fatalf("resolveScript: %v", err)
	}
	if got != "console.log('hello')" {
		t.Errorf("got %q, want inline value", got)
	}
}

func TestResolveScript_FileRef(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "test.js"), []byte("  var x = 1;  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test:\n  js: \"@scripts/test.js\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	got, err := resolveScript("test.js")
	if err != nil {
		t.Fatalf("resolveScript: %v", err)
	}
	if got != "var x = 1;" {
		t.Errorf("got %q, want trimmed file content", got)
	}
}

func TestResolveScript_MissingFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test:\n  js: \"@scripts/missing.js\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	_, err := resolveScript("test.js")
	if err == nil {
		t.Fatal("expected error for missing file reference")
	}
}

func TestResolveScript_NotConfigured(t *testing.T) {
	viper.Reset()

	_, err := resolveScript("nonexistent.key")
	if err == nil {
		t.Fatal("expected error for unconfigured key")
	}
	if got := err.Error(); got != "nonexistent.key not configured" {
		t.Errorf("error = %q", got)
	}
}

func TestResolveScript_FileRefNoConfig(t *testing.T) {
	viper.Reset()
	viper.Set("test.js", "@scripts/foo.js")

	_, err := resolveScript("test.js")
	if err == nil {
		t.Fatal("expected error when @ ref used without config file")
	}
}
