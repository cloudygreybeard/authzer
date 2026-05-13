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

// Package azcli wraps the Azure CLI (az) for querying Entra ID and
// Azure RBAC state. It is intentionally thin — all it does is exec
// `az` subcommands and parse JSON output.
package azcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps az CLI invocations.
type Client struct {
	AzBin string // path to az binary; defaults to "az"
}

// New returns a Client that uses the az binary at the given path.
// Pass "" to use the default "az" on PATH.
func New(azBin string) *Client {
	if azBin == "" {
		azBin = "az"
	}
	return &Client{AzBin: azBin}
}

// GroupMembership represents an Entra ID group the signed-in user
// belongs to.
type GroupMembership struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
}

// RoleAssignment represents an Azure RBAC role assignment.
type RoleAssignment struct {
	ID               string `json:"id"`
	RoleDefinitionID string `json:"roleDefinitionId"`
	RoleName         string `json:"roleDefinitionName"`
	Scope            string `json:"scope"`
	PrincipalName    string `json:"principalName"`
	PrincipalType    string `json:"principalType"`
}

// SignedInUser returns the object ID and UPN of the current az login.
type SignedInUser struct {
	ID  string `json:"id"`
	UPN string `json:"userPrincipalName"`
}

// WhoAmI returns the signed-in user's Entra object.
func (c *Client) WhoAmI(ctx context.Context) (*SignedInUser, error) {
	out, err := c.run(ctx, "ad", "signed-in-user", "show", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("az ad signed-in-user show: %w", err)
	}
	var u SignedInUser
	if err := json.Unmarshal(out, &u); err != nil {
		return nil, fmt.Errorf("parsing signed-in-user: %w", err)
	}
	return &u, nil
}

// ListGroups returns the Entra ID groups the signed-in user is a member of.
func (c *Client) ListGroups(ctx context.Context) ([]GroupMembership, error) {
	out, err := c.run(ctx,
		"ad", "signed-in-user", "list-owned-objects",
		"--type", "group",
		"--output", "json",
	)
	if err != nil {
		// Fall back to memberOf which works more broadly.
		out, err = c.run(ctx,
			"rest",
			"--method", "GET",
			"--url", "https://graph.microsoft.com/v1.0/me/memberOf/microsoft.graph.group",
			"--output", "json",
		)
		if err != nil {
			return nil, fmt.Errorf("listing group memberships: %w", err)
		}
		var wrapper struct {
			Value []GroupMembership `json:"value"`
		}
		if err := json.Unmarshal(out, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing graph groups: %w", err)
		}
		return wrapper.Value, nil
	}
	var groups []GroupMembership
	if err := json.Unmarshal(out, &groups); err != nil {
		return nil, fmt.Errorf("parsing groups: %w", err)
	}
	return groups, nil
}

// ListRoleAssignments returns Azure RBAC role assignments for the
// signed-in user. If scope is non-empty, results are filtered to that
// scope (e.g. a subscription or resource group path).
func (c *Client) ListRoleAssignments(ctx context.Context, scope string) ([]RoleAssignment, error) {
	args := []string{"role", "assignment", "list", "--assignee", "@me", "--output", "json"}
	if scope != "" {
		args = append(args, "--scope", scope)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("listing role assignments: %w", err)
	}
	var assignments []RoleAssignment
	if err := json.Unmarshal(out, &assignments); err != nil {
		return nil, fmt.Errorf("parsing role assignments: %w", err)
	}
	return assignments, nil
}

// CheckGroupMembership verifies the signed-in user is a member of the
// group identified by groupID (object ID or display name).
func (c *Client) CheckGroupMembership(ctx context.Context, groupID string) (bool, error) {
	groups, err := c.ListGroups(ctx)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		if strings.EqualFold(g.ID, groupID) || strings.EqualFold(g.DisplayName, groupID) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.AzBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
