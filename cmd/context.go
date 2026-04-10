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
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// activeContext holds the resolved context name after initConfig runs.
// Empty in flat mode (no registry).
var activeContext string

// registryPath returns the path to the context registry file.
func registryPath() string {
	return filepath.Join(xdgConfigHome(), "authzer", "contexts.yaml")
}

// loadRegistry reads and parses the context registry. Returns nil, nil
// if the file does not exist.
func loadRegistry() (*ContextRegistry, error) {
	path := registryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading registry: %w", err)
	}

	var reg ContextRegistry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}
	return &reg, nil
}

// saveRegistry writes the context registry to disk.
func saveRegistry(reg *ContextRegistry) error {
	if reg.APIVersion == "" {
		reg.APIVersion = APIVersion
	}
	if reg.Kind == "" {
		reg.Kind = "ContextList"
	}

	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}

	path := registryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating registry directory: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// resolveContextDir determines the config directory for the given
// context name from the registry. The path is resolved relative to the
// authzer config root unless it is absolute.
func resolveContextDir(reg *ContextRegistry, name string) (string, error) {
	for _, entry := range reg.Contexts {
		if entry.Name == name {
			p := entry.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(xdgConfigHome(), "authzer", p)
			}
			return p, nil
		}
	}
	names := make([]string, 0, len(reg.Contexts))
	for _, e := range reg.Contexts {
		names = append(names, e.Name)
	}
	return "", fmt.Errorf("context %q not found; available: %v", name, names)
}

// registerContext adds or updates a context entry in the registry. If
// setCurrent is true, the context becomes the current-context.
func registerContext(name, path string, setCurrent bool) error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		reg = &ContextRegistry{
			TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ContextList"},
		}
	}

	found := false
	for i, entry := range reg.Contexts {
		if entry.Name == name {
			reg.Contexts[i].Path = path
			found = true
			break
		}
	}
	if !found {
		reg.Contexts = append(reg.Contexts, ContextEntry{Name: name, Path: path})
	}

	if setCurrent || reg.CurrentContext == "" {
		reg.CurrentContext = name
	}

	return saveRegistry(reg)
}
