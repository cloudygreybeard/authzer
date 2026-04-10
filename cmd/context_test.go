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

	"gopkg.in/yaml.v3"
)

func TestContextRegistryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg := &ContextRegistry{
		TypeMeta:       TypeMeta{APIVersion: APIVersion, Kind: "ContextList"},
		CurrentContext: "alpha",
		Contexts: []ContextEntry{
			{Name: "alpha", Path: "alpha"},
			{Name: "beta", Path: "beta"},
		},
	}

	if err := saveRegistry(reg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadRegistry()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded registry is nil")
	}
	if loaded.CurrentContext != "alpha" {
		t.Errorf("current-context = %q, want alpha", loaded.CurrentContext)
	}
	if len(loaded.Contexts) != 2 {
		t.Fatalf("contexts count = %d, want 2", len(loaded.Contexts))
	}
	if loaded.Contexts[0].Name != "alpha" || loaded.Contexts[1].Name != "beta" {
		t.Errorf("contexts = %+v", loaded.Contexts)
	}
}

func TestLoadRegistry_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg, err := loadRegistry()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg != nil {
		t.Error("expected nil registry when file does not exist")
	}
}

func TestResolveContextDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg := &ContextRegistry{
		Contexts: []ContextEntry{
			{Name: "myctx", Path: "myctx"},
		},
	}

	got, err := resolveContextDir(reg, "myctx")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(dir, "authzer", "myctx")
	if got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
}

func TestResolveContextDir_AbsPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	absPath := filepath.Join(dir, "elsewhere")
	reg := &ContextRegistry{
		Contexts: []ContextEntry{
			{Name: "ext", Path: absPath},
		},
	}

	got, err := resolveContextDir(reg, "ext")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != absPath {
		t.Errorf("dir = %q, want %q", got, absPath)
	}
}

func TestResolveContextDir_NotFound(t *testing.T) {
	reg := &ContextRegistry{
		Contexts: []ContextEntry{
			{Name: "alpha", Path: "alpha"},
		},
	}

	_, err := resolveContextDir(reg, "missing")
	if err == nil {
		t.Error("expected error for missing context")
	}
}

func TestRegisterContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := registerContext("first", "first", true); err != nil {
		t.Fatalf("register first: %v", err)
	}

	reg, err := loadRegistry()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if reg.CurrentContext != "first" {
		t.Errorf("current = %q, want first", reg.CurrentContext)
	}
	if len(reg.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(reg.Contexts))
	}

	if err := registerContext("second", "second", false); err != nil {
		t.Fatalf("register second: %v", err)
	}

	reg, _ = loadRegistry()
	if reg.CurrentContext != "first" {
		t.Errorf("current should still be first, got %q", reg.CurrentContext)
	}
	if len(reg.Contexts) != 2 {
		t.Fatalf("contexts = %d, want 2", len(reg.Contexts))
	}
}

func TestRegisterContext_Update(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	_ = registerContext("ctx", "old-path", true)
	_ = registerContext("ctx", "new-path", false)

	reg, _ := loadRegistry()
	if len(reg.Contexts) != 1 {
		t.Fatalf("expected 1 context after update, got %d", len(reg.Contexts))
	}
	if reg.Contexts[0].Path != "new-path" {
		t.Errorf("path = %q, want new-path", reg.Contexts[0].Path)
	}
}

func TestContextRegistryParse(t *testing.T) {
	raw := `
apiVersion: authzer/v1alpha1
kind: ContextList
current-context: prod
contexts:
  - name: prod
    path: prod
  - name: staging
    path: staging
`
	var reg ContextRegistry
	if err := yaml.Unmarshal([]byte(raw), &reg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if reg.Kind != "ContextList" {
		t.Errorf("kind = %q", reg.Kind)
	}
	if reg.CurrentContext != "prod" {
		t.Errorf("current = %q", reg.CurrentContext)
	}
	if len(reg.Contexts) != 2 {
		t.Fatalf("contexts = %d", len(reg.Contexts))
	}
}

func TestFlatModeBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	authzerDir := filepath.Join(dir, "authzer")
	if err := os.MkdirAll(authzerDir, 0755); err != nil {
		t.Fatal(err)
	}
	configContent := "apiVersion: authzer/v1alpha1\nkind: Config\ngroup: sre\n"
	if err := os.WriteFile(filepath.Join(authzerDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := loadRegistry()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if reg != nil {
		t.Error("expected nil registry in flat mode")
	}
}
