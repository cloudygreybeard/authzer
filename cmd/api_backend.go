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

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// loadAPIBackend reads the APIBackend document from the context
// directory. It looks for the file named in the "apiBackend" config
// key, defaulting to "api-backend.yaml". Relative paths are resolved
// against the directory containing the active config file.
//
// For backward compatibility, both "APIBackend" and "ApiBackend" are
// accepted as the kind value.
func loadAPIBackend() (*APIBackend, error) {
	ref := viper.GetString("apiBackend")
	if ref == "" {
		ref = "api-backend.yaml"
	}

	path := ref
	if !filepath.IsAbs(path) {
		configFile := viper.ConfigFileUsed()
		if configFile != "" {
			path = filepath.Join(filepath.Dir(configFile), path)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading API backend %s: %w", path, err)
	}

	var ab APIBackend
	if err := yaml.Unmarshal(data, &ab); err != nil {
		return nil, fmt.Errorf("parsing API backend %s: %w", path, err)
	}

	if ab.Kind != "APIBackend" && ab.Kind != "ApiBackend" {
		return nil, fmt.Errorf("expected kind: APIBackend, got %q in %s", ab.Kind, path)
	}
	if ab.APIVersion != "" && ab.APIVersion != APIVersion {
		return nil, fmt.Errorf("unsupported apiVersion %q in %s (expected %s)", ab.APIVersion, path, APIVersion)
	}

	if ab.Spec.BaseURL == "" {
		return nil, fmt.Errorf("spec.baseURL is required in %s", path)
	}
	if ab.Spec.Endpoints.List.Path == "" {
		return nil, fmt.Errorf("spec.endpoints.list.path is required in %s", path)
	}
	if ab.Spec.Endpoints.Submit.Path == "" {
		return nil, fmt.Errorf("spec.endpoints.submit.path is required in %s", path)
	}

	return &ab, nil
}
