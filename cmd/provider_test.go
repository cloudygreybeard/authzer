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
	"testing"
)

type stubProvider struct {
	name         string
	kinds        []string
	capabilities ProviderCapability
	assignments  []Assignment
	listErr      error
	checkResult  *CheckResult
	checkErr     error
	applyResult  *ApplyResult
	applyErr     error
}

func (s *stubProvider) Name() string                     { return s.name }
func (s *stubProvider) Kinds() []string                  { return s.kinds }
func (s *stubProvider) Capabilities() ProviderCapability { return s.capabilities }
func (s *stubProvider) List(_ context.Context) ([]Assignment, error) {
	return s.assignments, s.listErr
}
func (s *stubProvider) Check(_ context.Context, rule Rule) (*CheckResult, error) {
	if s.checkResult != nil {
		return s.checkResult, s.checkErr
	}
	return &CheckResult{Rule: rule}, s.checkErr
}
func (s *stubProvider) Apply(_ context.Context, rule Rule, _ string, _ bool) (*ApplyResult, error) {
	if s.applyResult != nil {
		return s.applyResult, s.applyErr
	}
	return &ApplyResult{Rule: rule, Action: "created"}, s.applyErr
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	reg := NewRegistry()

	entra := &stubProvider{name: "entra", kinds: []string{"EntraGroup"}}
	rbac := &stubProvider{name: "rbac", kinds: []string{"AzureRoleAssignment"}}
	reg.Register(entra)
	reg.Register(rbac)

	p, err := reg.ForKind("EntraGroup")
	if err != nil {
		t.Fatalf("ForKind(EntraGroup): %v", err)
	}
	if p.Name() != "entra" {
		t.Errorf("expected provider name 'entra', got %q", p.Name())
	}

	p, err = reg.ForKind("AzureRoleAssignment")
	if err != nil {
		t.Fatalf("ForKind(AzureRoleAssignment): %v", err)
	}
	if p.Name() != "rbac" {
		t.Errorf("expected provider name 'rbac', got %q", p.Name())
	}

	_, err = reg.ForKind("Unknown")
	if err == nil {
		t.Error("expected error for unknown kind, got nil")
	}
}

func TestRulesByKind(t *testing.T) {
	rules := []Rule{
		{Kind: "Entitlement", Resource: "a"},
		{Kind: "EntraGroup", Resource: "b"},
		{Kind: "", Resource: "c"},
		{Kind: "AzureRoleAssignment", Resource: "d"},
		{Kind: "Entitlement", Resource: "e"},
	}

	m := RulesByKind(rules, "Entitlement")

	if len(m["Entitlement"]) != 3 {
		t.Errorf("expected 3 Entitlement rules (incl. default), got %d", len(m["Entitlement"]))
	}
	if len(m["EntraGroup"]) != 1 {
		t.Errorf("expected 1 EntraGroup rule, got %d", len(m["EntraGroup"]))
	}
	if len(m["AzureRoleAssignment"]) != 1 {
		t.Errorf("expected 1 AzureRoleAssignment rule, got %d", len(m["AzureRoleAssignment"]))
	}
}

func TestCacheOnlyProviderApplyReturnsError(t *testing.T) {
	p := NewCacheOnlyProvider(t.TempDir(), nil)
	ctx := context.Background()

	_, err := p.Apply(ctx, Rule{Resource: "x"}, "", false)
	if err == nil {
		t.Error("expected error from CacheOnlyProvider.Apply, got nil")
	}
}

func TestCacheOnlyProviderCapabilities(t *testing.T) {
	p := NewCacheOnlyProvider(t.TempDir(), nil)
	if p.Capabilities()&CapApply != 0 {
		t.Error("CacheOnlyProvider should not have CapApply")
	}
	if p.Capabilities()&CapList == 0 {
		t.Error("CacheOnlyProvider should have CapList")
	}
	if p.Capabilities()&CapCheck == 0 {
		t.Error("CacheOnlyProvider should have CapCheck")
	}
}

func TestRegistryKinds(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubProvider{name: "a", kinds: []string{"X", "Y"}})
	reg.Register(&stubProvider{name: "b", kinds: []string{"Z"}})

	kinds := reg.Kinds()
	if len(kinds) != 3 {
		t.Errorf("expected 3 kinds, got %d: %v", len(kinds), kinds)
	}
}

func TestCapabilityBitmask(t *testing.T) {
	full := CapList | CapCheck | CapApply
	if full&CapList == 0 {
		t.Error("expected CapList set")
	}
	if full&CapCheck == 0 {
		t.Error("expected CapCheck set")
	}
	if full&CapApply == 0 {
		t.Error("expected CapApply set")
	}

	readOnly := CapList | CapCheck
	if readOnly&CapApply != 0 {
		t.Error("expected CapApply unset for read-only provider")
	}
}
