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
	"testing"
)

func TestMultiProviderPolicyParsing(t *testing.T) {
	data, err := os.ReadFile("testdata/policy-multi-provider.yaml")
	if err != nil {
		t.Fatalf("reading test policy: %v", err)
	}

	policy, err := parsePolicy(data)
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	if len(policy.Roles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(policy.Roles))
	}
	if len(policy.Groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(policy.Groups))
	}
	if len(policy.RoleBindings) != 3 {
		t.Errorf("expected 3 role bindings, got %d", len(policy.RoleBindings))
	}

	rules, justification, err := policy.Resolve("sre")
	if err != nil {
		t.Fatalf("Resolve(sre): %v", err)
	}

	if justification != "Platform SRE team member" {
		t.Errorf("unexpected justification: %q", justification)
	}

	byKind := RulesByKind(rules, "Entitlement")

	if len(byKind["Entitlement"]) != 2 {
		t.Errorf("expected 2 Entitlement rules, got %d", len(byKind["Entitlement"]))
	}
	if len(byKind["EntraGroup"]) != 3 {
		t.Errorf("expected 3 EntraGroup rules, got %d", len(byKind["EntraGroup"]))
	}
	if len(byKind["AzureRoleAssignment"]) != 2 {
		t.Errorf("expected 2 AzureRoleAssignment rules, got %d", len(byKind["AzureRoleAssignment"]))
	}

	for _, r := range byKind["EntraGroup"] {
		if r.Permission != "member" {
			t.Errorf("EntraGroup rule %s: expected permission 'member', got %q", r.Resource, r.Permission)
		}
	}

	for _, r := range byKind["AzureRoleAssignment"] {
		if r.Permission == "" {
			t.Errorf("AzureRoleAssignment rule %s: expected non-empty permission", r.Resource)
		}
	}
}
