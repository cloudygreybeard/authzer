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
	"strings"

	"github.com/cloudygreybeard/authzer/internal/azcli"
)

// AzureRBACProvider checks Azure role assignments via the az CLI.
// It supports List and Check. Apply is stubbed — creating role
// assignments requires Owner or User Access Administrator on the target
// scope, which is unusual for an SRE's own identity.
type AzureRBACProvider struct {
	client *azcli.Client
	scope  string // optional scope filter (subscription or resource group)
}

// NewAzureRBACProvider returns a provider for Azure RBAC role assignments.
// If scope is non-empty, listings and checks are restricted to that scope.
func NewAzureRBACProvider(client *azcli.Client, scope string) *AzureRBACProvider {
	return &AzureRBACProvider{client: client, scope: scope}
}

func (p *AzureRBACProvider) Name() string                     { return "azure-rbac" }
func (p *AzureRBACProvider) Kinds() []string                  { return []string{"AzureRoleAssignment"} }
func (p *AzureRBACProvider) Capabilities() ProviderCapability { return CapList | CapCheck }

func (p *AzureRBACProvider) List(ctx context.Context) ([]Assignment, error) {
	assignments, err := p.client.ListRoleAssignments(ctx, p.scope)
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, len(assignments))
	for i, a := range assignments {
		out[i] = Assignment{
			Kind:  "AzureRoleAssignment",
			ID:    a.ID,
			Name:  fmt.Sprintf("%s @ %s", a.RoleName, scopeShortName(a.Scope)),
			Role:  a.RoleName,
			State: "active",
		}
	}
	return out, nil
}

func (p *AzureRBACProvider) Check(ctx context.Context, rule Rule) (*CheckResult, error) {
	assignments, err := p.client.ListRoleAssignments(ctx, p.scope)
	if err != nil {
		return nil, err
	}

	for _, a := range assignments {
		if matchesRoleAssignment(a, rule) {
			return &CheckResult{
				Rule:      rule,
				Satisfied: true,
				Message:   fmt.Sprintf("role %s assigned at %s", a.RoleName, scopeShortName(a.Scope)),
				Assignment: &Assignment{
					Kind:  "AzureRoleAssignment",
					ID:    a.ID,
					Name:  fmt.Sprintf("%s @ %s", a.RoleName, scopeShortName(a.Scope)),
					Role:  a.RoleName,
					State: "active",
				},
			}, nil
		}
	}

	return &CheckResult{
		Rule:    rule,
		Message: fmt.Sprintf("role %s not found at scope %s", rule.Permission, rule.Resource),
	}, nil
}

func (p *AzureRBACProvider) Apply(_ context.Context, rule Rule, _ string, _ bool) (*ApplyResult, error) {
	return &ApplyResult{
		Rule:    rule,
		Action:  "failed",
		Message: "AzureRoleAssignment creation requires Owner or User Access Administrator; use PIM or the portal",
		Error:   fmt.Errorf("apply not supported for kind AzureRoleAssignment"),
	}, nil
}

// matchesRoleAssignment checks whether an Azure role assignment
// satisfies a policy rule. The rule's Resource carries the scope (or a
// suffix of it), and Permission carries the desired role name.
func matchesRoleAssignment(a azcli.RoleAssignment, rule Rule) bool {
	roleMatch := strings.EqualFold(a.RoleName, rule.Permission)
	scopeMatch := strings.EqualFold(a.Scope, rule.Resource) ||
		strings.HasSuffix(strings.ToLower(a.Scope), strings.ToLower(rule.Resource))
	return roleMatch && scopeMatch
}

// scopeShortName returns a human-readable abbreviated scope.
func scopeShortName(scope string) string {
	parts := strings.Split(scope, "/")
	if len(parts) >= 3 {
		// /subscriptions/SUB_ID/resourceGroups/RG -> RG
		// /subscriptions/SUB_ID -> SUB_ID (first 8 chars)
		last := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]
		switch strings.ToLower(secondLast) {
		case "subscriptions":
			if len(last) > 8 {
				return last[:8] + "..."
			}
			return last
		case "resourcegroups", "providers":
			return last
		}
	}
	if len(scope) > 40 {
		return "..." + scope[len(scope)-37:]
	}
	return scope
}
