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
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// loadPolicy reads RBAC resources from the policy file referenced by
// the "policy" config key. The file may contain either multi-document
// YAML (---separated) or a single kind: List document. Returns an
// empty Policy if no policy file is configured.
func loadPolicy() (*Policy, error) {
	policyRef := viper.GetString("policy")
	if policyRef == "" {
		return &Policy{
			Roles:  make(map[string]*Role),
			Groups: make(map[string]*Group),
		}, nil
	}

	policyPath := policyRef
	if !filepath.IsAbs(policyPath) {
		configFile := viper.ConfigFileUsed()
		if configFile != "" {
			policyPath = filepath.Join(filepath.Dir(configFile), policyPath)
		}
	}

	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("reading policy file %s: %w", policyPath, err)
	}

	return parsePolicy(data)
}

// parsePolicy decodes RBAC resources from raw YAML. Supports both
// multi-document YAML and kind: List wrapper.
func parsePolicy(data []byte) (*Policy, error) {
	p := &Policy{
		Roles:  make(map[string]*Role),
		Groups: make(map[string]*Group),
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding policy YAML: %w", err)
		}

		var meta TypeMeta
		if err := node.Decode(&meta); err != nil {
			return nil, fmt.Errorf("decoding resource meta: %w", err)
		}

		if meta.Kind == "List" {
			var list ResourceList
			if err := node.Decode(&list); err != nil {
				return nil, fmt.Errorf("decoding List: %w", err)
			}
			for i := range list.Items {
				if err := p.addResource(&list.Items[i]); err != nil {
					return nil, err
				}
			}
			continue
		}

		if err := p.addResource(&node); err != nil {
			return nil, err
		}
	}

	resolveAggregation(p.Roles)
	return p, nil
}

// addResource dispatches a single YAML node into the appropriate typed
// map based on its kind field.
func (p *Policy) addResource(node *yaml.Node) error {
	var meta TypeMeta
	if err := node.Decode(&meta); err != nil {
		return fmt.Errorf("decoding resource meta: %w", err)
	}

	switch meta.Kind {
	case "Role":
		var role Role
		if err := node.Decode(&role); err != nil {
			return fmt.Errorf("decoding Role: %w", err)
		}
		p.Roles[role.Metadata.Name] = &role
	case "Group":
		var group Group
		if err := node.Decode(&group); err != nil {
			return fmt.Errorf("decoding Group: %w", err)
		}
		p.Groups[group.Metadata.Name] = &group
	case "RoleBinding":
		var rb RoleBinding
		if err := node.Decode(&rb); err != nil {
			return fmt.Errorf("decoding RoleBinding: %w", err)
		}
		p.RoleBindings = append(p.RoleBindings, &rb)
	default:
		return fmt.Errorf("unknown resource kind %q", meta.Kind)
	}
	return nil
}

// resolveAggregation merges rules from matching Roles into aggregate
// Roles. An aggregate Role has an AggregationRule with label selectors;
// its Rules are replaced by the union of rules from all non-aggregate
// Roles whose labels match.
func resolveAggregation(roles map[string]*Role) {
	for _, role := range roles {
		if role.AggregationRule == nil {
			continue
		}
		var merged []Rule
		for _, other := range roles {
			if other.AggregationRule != nil {
				continue
			}
			if matchesAny(other.Metadata.Labels, role.AggregationRule.RoleSelectors) {
				merged = append(merged, other.Rules...)
			}
		}
		role.Rules = merged
	}
}

func matchesAny(labels map[string]string, selectors []LabelSelector) bool {
	for _, sel := range selectors {
		if matchesAll(labels, sel.MatchLabels) {
			return true
		}
	}
	return false
}

func matchesAll(labels, required map[string]string) bool {
	for k, v := range required {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// Resolve resolves a group name through the RBAC policy to produce a
// flat list of rules and the group's justification text. The resolution
// path is: Group -> RoleBindings (by subject) -> Roles -> Rules.
func (p *Policy) Resolve(group string) ([]Rule, string, error) {
	grp, ok := p.Groups[group]
	if !ok {
		available := make([]string, 0, len(p.Groups))
		for k := range p.Groups {
			available = append(available, k)
		}
		return nil, "", fmt.Errorf("group %q not found in policy (available: %s)",
			group, strings.Join(available, ", "))
	}

	var roleNames []string
	for _, rb := range p.RoleBindings {
		for _, subj := range rb.Subjects {
			if subj.Kind == "Group" && subj.Name == group {
				roleNames = append(roleNames, rb.RoleRef.Name)
				break
			}
		}
	}
	if len(roleNames) == 0 {
		return nil, "", fmt.Errorf("no RoleBindings found for group %q", group)
	}

	seen := make(map[string]int)
	var rules []Rule
	for _, rn := range roleNames {
		role, ok := p.Roles[rn]
		if !ok {
			return nil, "", fmt.Errorf("Role %q (referenced by RoleBinding for group %q) not found", rn, group)
		}
		for _, r := range role.Rules {
			if idx, exists := seen[r.Resource]; exists {
				rules[idx] = r
			} else {
				seen[r.Resource] = len(rules)
				rules = append(rules, r)
			}
		}
	}

	return rules, grp.Justification, nil
}

// isExcludedRole returns true for permission options that match any
// pattern in portal.form.roleExcludePatterns (e.g. instruction text
// masquerading as a permission option).
func isExcludedRole(name string) bool {
	patterns := viper.GetStringSlice("portal.form.roleExcludePatterns")
	upper := strings.ToUpper(name)
	for _, pat := range patterns {
		if strings.Contains(upper, strings.ToUpper(pat)) {
			return true
		}
	}
	return false
}
