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
	"context"
	"fmt"

	"github.com/cloudygreybeard/authzer/internal/azcli"
	"github.com/spf13/viper"
)

// defaultRegistry is the global provider registry, initialised lazily.
var defaultRegistry *Registry

// initRegistry builds the global provider registry with cache-only
// defaults. The Entitlement kind is served by CacheOnlyProvider so
// that the status command works without a live browser. Commands that
// need a browser (get, apply) call initBrowserRegistry instead.
func initRegistry() *Registry {
	if defaultRegistry != nil {
		return defaultRegistry
	}
	reg := NewRegistry()

	reg.Register(NewCacheOnlyProvider(cacheDirectory(), nil))
	registerCLIProviders(reg)

	defaultRegistry = reg
	return reg
}

// initBrowserRegistry creates a registry with a live browser-backed
// provider for the Entitlement kind, plus any configured CLI providers.
// When backend is "api", an APIProvider is used; otherwise a CDPProvider
// is created from the browser context.
func initBrowserRegistry(browserCtx context.Context, opts surveyOpts) (*Registry, error) {
	reg := NewRegistry()

	backend := viper.GetString("backend")
	if err := validateBackend(backend); err != nil {
		return nil, err
	}

	if backend == "api" {
		ab, err := loadAPIBackend()
		if err != nil {
			return nil, fmt.Errorf("loading API backend: %w", err)
		}
		reg.Register(NewAPIProvider(ab, browserCtx, nil))
	} else {
		reg.Register(NewCDPProvider(browserCtx, opts))
	}

	registerCLIProviders(reg)
	return reg, nil
}

// registerCLIProviders adds Entra and Azure RBAC providers to the
// registry when they are enabled in config.
func registerCLIProviders(reg *Registry) {
	if viper.GetBool("providers.entra.enabled") {
		azBin := viper.GetString("providers.entra.azBin")
		client := azcli.New(azBin)
		reg.Register(NewEntraGroupProvider(client))
	}

	if viper.GetBool("providers.azureRBAC.enabled") {
		azBin := viper.GetString("providers.azureRBAC.azBin")
		scope := viper.GetString("providers.azureRBAC.scope")
		client := azcli.New(azBin)
		reg.Register(NewAzureRBACProvider(client, scope))
	}
}
