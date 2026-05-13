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
	"sync"
)

// ProviderCapability describes what a provider can do.
type ProviderCapability int

const (
	CapList  ProviderCapability = 1 << iota // enumerate current assignments
	CapCheck                                // verify a single assignment exists
	CapApply                                // create or renew an assignment
)

// CheckResult reports whether a specific rule is satisfied.
type CheckResult struct {
	Rule       Rule
	Satisfied  bool
	Assignment *Assignment
	Message    string
}

// ApplyResult reports the outcome of a reconciliation action.
type ApplyResult struct {
	Rule    Rule
	Action  string // "created", "renewed", "already-satisfied", "failed"
	Message string
	Error   error
}

// Provider is the interface that backends implement to list, check, and
// reconcile access assignments. Each provider handles one or more Rule
// kinds (e.g. "Entitlement", "EntraGroup", "AzureRoleAssignment").
//
// Not every provider supports every operation — Capabilities() declares
// what is available. Callers must check before invoking.
type Provider interface {
	// Name returns a human-readable identifier for log output.
	Name() string

	// Kinds returns the Rule.Kind values this provider handles.
	Kinds() []string

	// Capabilities returns the bitmask of supported operations.
	Capabilities() ProviderCapability

	// List enumerates all current assignments visible to the caller.
	// Providers that don't support listing return an empty slice and nil.
	List(ctx context.Context) ([]Assignment, error)

	// Check verifies whether a single rule is currently satisfied.
	Check(ctx context.Context, rule Rule) (*CheckResult, error)

	// Apply reconciles a single rule — creating, renewing, or
	// activating the assignment as needed. justification is passed
	// through for providers that accept it.
	Apply(ctx context.Context, rule Rule, justification string, dryRun bool) (*ApplyResult, error)
}

// Registry maps Rule.Kind values to their Provider implementation.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider, mapping each of its declared kinds.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range p.Kinds() {
		r.providers[k] = p
	}
}

// ForKind returns the provider for a given Rule.Kind, or an error if
// none is registered.
func (r *Registry) ForKind(kind string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[kind]
	if !ok {
		return nil, fmt.Errorf("no provider registered for kind %q", kind)
	}
	return p, nil
}

// Kinds returns all registered kind values.
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]string, 0, len(r.providers))
	for k := range r.providers {
		kinds = append(kinds, k)
	}
	return kinds
}

// RulesByKind partitions rules by their Kind field and returns a map
// keyed by kind. Rules with an empty Kind default to defaultKind.
func RulesByKind(rules []Rule, defaultKind string) map[string][]Rule {
	m := make(map[string][]Rule)
	for _, r := range rules {
		kind := r.Kind
		if kind == "" {
			kind = defaultKind
		}
		m[kind] = append(m[kind], r)
	}
	return m
}
