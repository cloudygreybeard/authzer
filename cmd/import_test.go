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

func TestParseSitePack(t *testing.T) {
	raw := `
apiVersion: authzer/v1alpha1
kind: SitePack
metadata:
  name: test-pack
  annotations:
    description: "A test pack"
values:
  - key: greeting
    prompt: "Greeting text"
    default: "hello"
  - key: target
    prompt: "Target name"
templates:
  config.txt: |
    message: {{ .greeting }} {{ .target }}
data:
  scripts/helper.js: |
    console.log("ok");
`
	var sp SitePack
	if err := yaml.Unmarshal([]byte(raw), &sp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sp.Kind != "SitePack" {
		t.Errorf("kind = %q, want SitePack", sp.Kind)
	}
	if sp.Metadata.Name != "test-pack" {
		t.Errorf("name = %q, want test-pack", sp.Metadata.Name)
	}
	if len(sp.Values) != 2 {
		t.Fatalf("values count = %d, want 2", len(sp.Values))
	}
	if sp.Values[0].Key != "greeting" || sp.Values[0].Default != "hello" {
		t.Errorf("values[0] = %+v", sp.Values[0])
	}
	if sp.Values[1].Key != "target" || sp.Values[1].Default != "" {
		t.Errorf("values[1] = %+v", sp.Values[1])
	}
	if len(sp.Templates) != 1 {
		t.Errorf("templates count = %d, want 1", len(sp.Templates))
	}
	if len(sp.Data) != 1 {
		t.Errorf("data count = %d, want 1", len(sp.Data))
	}
}

func TestRenderTemplate(t *testing.T) {
	vals := map[string]interface{}{
		"name":  "world",
		"count": "42",
	}

	content := "Hello {{ .name }}, count={{ .count }}"
	out, err := renderTemplate("test", content, vals)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Hello world, count=42"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestRenderTemplate_MissingKey(t *testing.T) {
	vals := map[string]interface{}{"name": "world"}
	_, err := renderTemplate("test", "{{ .missing }}", vals)
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestResolveValues_FromFile(t *testing.T) {
	dir := t.TempDir()
	vf := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(vf, []byte("greeting: hi\ntarget: earth\n"), 0644); err != nil {
		t.Fatal(err)
	}

	vals, err := resolveValues(nil, vf)
	if err != nil {
		t.Fatalf("resolveValues: %v", err)
	}
	if vals["greeting"] != "hi" {
		t.Errorf("greeting = %q, want hi", vals["greeting"])
	}
	if vals["target"] != "earth" {
		t.Errorf("target = %q, want earth", vals["target"])
	}
}

func TestWriteFileWithDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "file.txt")
	if err := writeFileWithDirs(path, []byte("content")); err != nil {
		t.Fatalf("writeFileWithDirs: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("got %q, want content", string(data))
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]string{"z": "", "a": "", "m": ""}
	keys := sortedKeys(m)
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
		t.Errorf("sortedKeys = %v, want [a m z]", keys)
	}
}

func TestImportEndToEnd(t *testing.T) {
	manifest := `
apiVersion: authzer/v1alpha1
kind: SitePack
metadata:
  name: e2e-test
templates:
  config.yaml: |
    name: {{ .app }}
    port: {{ .port }}
data:
  scripts/init.js: |
    console.log("init");
  readme.txt: |
    This is a readme.
`
	values := "app: myapp\nport: \"8080\"\n"

	dir := t.TempDir()
	mf := filepath.Join(dir, "pack.yaml")
	vf := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(mf, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vf, []byte(values), 0644); err != nil {
		t.Fatal(err)
	}

	var sp SitePack
	raw, _ := os.ReadFile(mf)
	if err := yaml.Unmarshal(raw, &sp); err != nil {
		t.Fatalf("parse: %v", err)
	}

	vals, err := resolveValues(sp.Values, vf)
	if err != nil {
		t.Fatalf("values: %v", err)
	}

	destDir := filepath.Join(dir, "output")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	for name, content := range sp.Templates {
		rendered, err := renderTemplate(name, content, vals)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if err := writeFileWithDirs(filepath.Join(destDir, name), []byte(rendered)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for name, content := range sp.Data {
		if err := writeFileWithDirs(filepath.Join(destDir, name), []byte(content)); err != nil {
			t.Fatalf("write data %s: %v", name, err)
		}
	}

	configData, err := os.ReadFile(filepath.Join(destDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(configData); got != "name: myapp\nport: 8080\n" {
		t.Errorf("config.yaml = %q", got)
	}

	jsData, err := os.ReadFile(filepath.Join(destDir, "scripts", "init.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(jsData); got != "console.log(\"init\");\n" {
		t.Errorf("init.js = %q", got)
	}

	readmeData, err := os.ReadFile(filepath.Join(destDir, "readme.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readmeData); got != "This is a readme.\n" {
		t.Errorf("readme.txt = %q", got)
	}
}
