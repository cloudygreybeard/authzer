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
)

// EntraGroupProvider checks Entra ID group memberships via the az CLI.
// It supports List and Check but not Apply — group membership changes
// require admin-level Graph API permissions that are out of scope for a
// user-level tool.
type EntraGroupProvider struct {
	client *azcli.Client
}

// NewEntraGroupProvider returns a provider backed by the given az CLI client.
func NewEntraGroupProvider(client *azcli.Client) *EntraGroupProvider {
	return &EntraGroupProvider{client: client}
}

func (p *EntraGroupProvider) Name() string                     { return "entra-groups" }
func (p *EntraGroupProvider) Kinds() []string                  { return []string{"EntraGroup"} }
func (p *EntraGroupProvider) Capabilities() ProviderCapability { return CapList | CapCheck }

func (p *EntraGroupProvider) List(ctx context.Context) ([]Assignment, error) {
	groups, err := p.client.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, len(groups))
	for i, g := range groups {
		out[i] = Assignment{
			Kind:  "EntraGroup",
			ID:    g.ID,
			Name:  g.DisplayName,
			State: "active",
		}
	}
	return out, nil
}

func (p *EntraGroupProvider) Check(ctx context.Context, rule Rule) (*CheckResult, error) {
	member, err := p.client.CheckGroupMembership(ctx, rule.Resource)
	if err != nil {
		return nil, err
	}
	result := &CheckResult{Rule: rule, Satisfied: member}
	if member {
		result.Message = fmt.Sprintf("member of group %s", rule.Resource)
		result.Assignment = &Assignment{
			Kind:  "EntraGroup",
			ID:    rule.Resource,
			Name:  rule.Resource,
			State: "active",
		}
	} else {
		result.Message = fmt.Sprintf("not a member of group %s", rule.Resource)
	}
	return result, nil
}

func (p *EntraGroupProvider) Apply(_ context.Context, rule Rule, _ string, _ bool) (*ApplyResult, error) {
	return &ApplyResult{
		Rule:    rule,
		Action:  "failed",
		Message: "EntraGroup assignments require admin Graph API permissions; use the Entra portal or PIM",
		Error:   fmt.Errorf("apply not supported for kind EntraGroup"),
	}, nil
}
