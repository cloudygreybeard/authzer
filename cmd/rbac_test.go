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
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// ---------------------------------------------------------------------------
// parsePolicy: multi-document YAML
// ---------------------------------------------------------------------------

func TestParsePolicy_MultiDocument(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: test-role
rules:
  - kind: Entitlement
    resource: res-1
    selfLink: https://example.com/res-1
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: eng
justification: "Engineering team"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: eng-test
subjects:
  - kind: Group
    name: eng
roleRef:
  kind: Role
  name: test-role
`

	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	if len(p.Roles) != 1 {
		t.Fatalf("expected 1 Role, got %d", len(p.Roles))
	}
	role := p.Roles["test-role"]
	if role == nil {
		t.Fatal("Role 'test-role' not found")
	}
	if len(role.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(role.Rules))
	}
	if role.Rules[0].Kind != "Entitlement" {
		t.Errorf("rule kind = %q, want %q", role.Rules[0].Kind, "Entitlement")
	}
	if role.Rules[0].Resource != "res-1" {
		t.Errorf("rule resource = %q, want %q", role.Rules[0].Resource, "res-1")
	}
	if role.Rules[0].Permission != "ReadOnly" {
		t.Errorf("rule permission = %q, want %q", role.Rules[0].Permission, "ReadOnly")
	}

	if len(p.Groups) != 1 {
		t.Fatalf("expected 1 Group, got %d", len(p.Groups))
	}
	grp := p.Groups["eng"]
	if grp == nil {
		t.Fatal("Group 'eng' not found")
	}
	if grp.Justification != "Engineering team" {
		t.Errorf("justification = %q, want %q", grp.Justification, "Engineering team")
	}

	if len(p.RoleBindings) != 1 {
		t.Fatalf("expected 1 RoleBinding, got %d", len(p.RoleBindings))
	}
	var rb *RoleBinding
	for _, b := range p.RoleBindings {
		if b.Metadata.Name == "eng-test" {
			rb = b
			break
		}
	}
	if rb == nil {
		t.Fatal("RoleBinding 'eng-test' not found")
	}
	if rb.RoleRef.Name != "test-role" {
		t.Errorf("roleRef name = %q, want %q", rb.RoleRef.Name, "test-role")
	}
	if len(rb.Subjects) != 1 || rb.Subjects[0].Name != "eng" {
		t.Errorf("unexpected subjects: %+v", rb.Subjects)
	}
}

// ---------------------------------------------------------------------------
// parsePolicy: kind: List wrapper
// ---------------------------------------------------------------------------

func TestParsePolicy_KindList(t *testing.T) {
	yaml := `apiVersion: authzer/v1alpha1
kind: List
items:
  - apiVersion: authzer/v1alpha1
    kind: Role
    metadata:
      name: list-role
    rules:
      - resource: r1
        selfLink: https://example.com/r1
        permission: Write
  - apiVersion: authzer/v1alpha1
    kind: Group
    metadata:
      name: ops
    justification: "Operations"
  - apiVersion: authzer/v1alpha1
    kind: RoleBinding
    metadata:
      name: ops-bind
    subjects:
      - kind: Group
        name: ops
    roleRef:
      kind: Role
      name: list-role
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}
	if len(p.Roles) != 1 {
		t.Errorf("expected 1 Role, got %d", len(p.Roles))
	}
	if len(p.Groups) != 1 {
		t.Errorf("expected 1 Group, got %d", len(p.Groups))
	}
	if len(p.RoleBindings) != 1 {
		t.Errorf("expected 1 RoleBinding, got %d", len(p.RoleBindings))
	}
}

func TestParsePolicy_UnknownKind(t *testing.T) {
	yaml := `apiVersion: authzer/v1alpha1
kind: Widget
metadata:
  name: w1
`
	_, err := parsePolicy([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if got := err.Error(); !contains(got, "unknown resource kind") {
		t.Errorf("error = %q, want substring %q", got, "unknown resource kind")
	}
}

func TestParsePolicy_Empty(t *testing.T) {
	p, err := parsePolicy([]byte(""))
	if err != nil {
		t.Fatalf("parsePolicy on empty input: %v", err)
	}
	if len(p.Roles) != 0 || len(p.Groups) != 0 || len(p.RoleBindings) != 0 {
		t.Error("expected empty policy from empty input")
	}
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

func TestResolveAggregation(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: logs-reader
  labels:
    team: sre
rules:
  - resource: logs-a
    selfLink: https://example.com/logs-a
    permission: ReadOnly
  - resource: logs-b
    selfLink: https://example.com/logs-b
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: devtools-user
  labels:
    team: sre
rules:
  - resource: dev-tools
    selfLink: https://example.com/dev-tools
    permission: Reader
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: unrelated
  labels:
    team: platform
rules:
  - resource: platform-x
    selfLink: https://example.com/platform-x
    permission: Admin
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: sre-all
aggregationRule:
  roleSelectors:
    - matchLabels:
        team: sre
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	agg := p.Roles["sre-all"]
	if agg == nil {
		t.Fatal("aggregate Role 'sre-all' not found")
	}
	if len(agg.Rules) != 3 {
		t.Fatalf("expected 3 aggregated rules, got %d", len(agg.Rules))
	}

	resources := make(map[string]bool)
	for _, r := range agg.Rules {
		resources[r.Resource] = true
	}
	for _, want := range []string{"logs-a", "logs-b", "dev-tools"} {
		if !resources[want] {
			t.Errorf("aggregated rules missing resource %q", want)
		}
	}
	if resources["platform-x"] {
		t.Error("aggregated rules should not include 'platform-x' (wrong label)")
	}
}

func TestResolveAggregation_NoMatch(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: some-role
  labels:
    team: backend
rules:
  - resource: api
    selfLink: https://example.com/api
    permission: ReadWrite
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: agg-frontend
aggregationRule:
  roleSelectors:
    - matchLabels:
        team: frontend
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}
	agg := p.Roles["agg-frontend"]
	if len(agg.Rules) != 0 {
		t.Errorf("expected 0 rules for non-matching aggregate, got %d", len(agg.Rules))
	}
}

func TestResolveAggregation_MultipleSelectors(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: r1
  labels:
    tier: logging
rules:
  - resource: log-store
    selfLink: https://example.com/log-store
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: r2
  labels:
    tier: monitoring
rules:
  - resource: metrics
    selfLink: https://example.com/metrics
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: observability
aggregationRule:
  roleSelectors:
    - matchLabels:
        tier: logging
    - matchLabels:
        tier: monitoring
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}
	agg := p.Roles["observability"]
	if len(agg.Rules) != 2 {
		t.Fatalf("expected 2 rules from multi-selector aggregate, got %d", len(agg.Rules))
	}
}

// ---------------------------------------------------------------------------
// Label matching
// ---------------------------------------------------------------------------

func TestMatchesAll(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		required map[string]string
		want     bool
	}{
		{"exact match", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"superset ok", map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}, true},
		{"missing key", map[string]string{"a": "1"}, map[string]string{"b": "1"}, false},
		{"wrong value", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"empty required", map[string]string{"a": "1"}, map[string]string{}, true},
		{"nil labels", nil, map[string]string{"a": "1"}, false},
		{"both nil", nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAll(tt.labels, tt.required); got != tt.want {
				t.Errorf("matchesAll(%v, %v) = %v, want %v", tt.labels, tt.required, got, tt.want)
			}
		})
	}
}

func TestMatchesAny(t *testing.T) {
	labels := map[string]string{"team": "sre", "tier": "logging"}

	tests := []struct {
		name      string
		selectors []LabelSelector
		want      bool
	}{
		{"single match", []LabelSelector{{MatchLabels: map[string]string{"team": "sre"}}}, true},
		{"second matches", []LabelSelector{
			{MatchLabels: map[string]string{"team": "backend"}},
			{MatchLabels: map[string]string{"tier": "logging"}},
		}, true},
		{"none match", []LabelSelector{{MatchLabels: map[string]string{"team": "backend"}}}, false},
		{"empty selectors", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAny(labels, tt.selectors); got != tt.want {
				t.Errorf("matchesAny = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Policy.Resolve
// ---------------------------------------------------------------------------

func TestResolve_FullChain(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: access-bundle
rules:
  - kind: Entitlement
    resource: logs
    selfLink: https://example.com/logs
    permission: ReadOnly
  - kind: Entitlement
    resource: dev-tools
    selfLink: https://example.com/dev-tools
    permission: Reader
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: eng
justification: "Engineering team member"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: eng-access
subjects:
  - kind: Group
    name: eng
roleRef:
  kind: Role
  name: access-bundle
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	rules, justification, err := p.Resolve("eng")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if justification != "Engineering team member" {
		t.Errorf("justification = %q, want %q", justification, "Engineering team member")
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Resource != "logs" || rules[0].Permission != "ReadOnly" {
		t.Errorf("rule[0] = %+v", rules[0])
	}
	if rules[1].Resource != "dev-tools" || rules[1].Permission != "Reader" {
		t.Errorf("rule[1] = %+v", rules[1])
	}
}

func TestResolve_MultipleBindings(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: role-a
rules:
  - resource: res-a
    selfLink: https://example.com/a
    permission: Read
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: role-b
rules:
  - resource: res-b
    selfLink: https://example.com/b
    permission: Write
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: team
justification: "Team access"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: bind-a
subjects:
  - kind: Group
    name: team
roleRef:
  kind: Role
  name: role-a
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: bind-b
subjects:
  - kind: Group
    name: team
roleRef:
  kind: Role
  name: role-b
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	rules, _, err := p.Resolve("team")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules from 2 bindings, got %d", len(rules))
	}
}

func TestResolve_LastWriteWins(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: role-x
rules:
  - resource: shared
    selfLink: https://example.com/shared
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: role-y
rules:
  - resource: shared
    selfLink: https://example.com/shared
    permission: ReadWrite
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: grp
justification: "test"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: bind-xy
subjects:
  - kind: Group
    name: grp
roleRef:
  kind: Role
  name: role-x
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: bind-xy2
subjects:
  - kind: Group
    name: grp
roleRef:
  kind: Role
  name: role-y
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	rules, _, err := p.Resolve("grp")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 deduplicated rule, got %d", len(rules))
	}
	if rules[0].Permission != "ReadWrite" {
		t.Errorf("expected last-write-wins permission 'ReadWrite', got %q", rules[0].Permission)
	}
}

func TestResolve_WithAggregation(t *testing.T) {
	yaml := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: individual-1
  labels:
    bundle: sre
rules:
  - resource: r1
    selfLink: https://example.com/r1
    permission: Read
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: individual-2
  labels:
    bundle: sre
rules:
  - resource: r2
    selfLink: https://example.com/r2
    permission: Write
---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: sre-aggregate
aggregationRule:
  roleSelectors:
    - matchLabels:
        bundle: sre
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: sre
justification: "SRE team"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: sre-bind
subjects:
  - kind: Group
    name: sre
roleRef:
  kind: Role
  name: sre-aggregate
`
	p, err := parsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	rules, justification, err := p.Resolve("sre")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if justification != "SRE team" {
		t.Errorf("justification = %q", justification)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules via aggregation, got %d", len(rules))
	}
}

// ---------------------------------------------------------------------------
// Resolve error cases
// ---------------------------------------------------------------------------

func TestResolve_MissingGroup(t *testing.T) {
	p := &Policy{
		Roles:  map[string]*Role{},
		Groups: map[string]*Group{},
	}
	_, _, err := p.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing group")
	}
	if !contains(err.Error(), "not found in policy") {
		t.Errorf("error = %q", err)
	}
}

func TestResolve_NoBindings(t *testing.T) {
	p := &Policy{
		Roles: map[string]*Role{},
		Groups: map[string]*Group{
			"eng": {Justification: "Engineering"},
		},
	}
	_, _, err := p.Resolve("eng")
	if err == nil {
		t.Fatal("expected error for missing bindings")
	}
	if !contains(err.Error(), "no RoleBindings found") {
		t.Errorf("error = %q", err)
	}
}

func TestResolve_MissingRole(t *testing.T) {
	p := &Policy{
		Roles: map[string]*Role{},
		Groups: map[string]*Group{
			"eng": {Justification: "Engineering"},
		},
		RoleBindings: []*RoleBinding{
			{
				Subjects: []Subject{{Kind: "Group", Name: "eng"}},
				RoleRef:  RoleRef{Kind: "Role", Name: "ghost"},
			},
		},
	}
	_, _, err := p.Resolve("eng")
	if err == nil {
		t.Fatal("expected error for missing role")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("error = %q", err)
	}
}

// ---------------------------------------------------------------------------
// isExcludedRole
// ---------------------------------------------------------------------------

func TestIsExcludedRole(t *testing.T) {
	viper.Set("portal.form.roleExcludePatterns", []string{
		"INSTRUCTIONS FOR MANAGER",
		"DO NOT SELECT",
	})
	defer viper.Reset()

	tests := []struct {
		name string
		want bool
	}{
		{"ReadOnly", false},
		{"ReadWrite", false},
		{"INSTRUCTIONS FOR MANAGER APPROVAL", true},
		{"instructions for manager", true},
		{"Please do not select this", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExcludedRole(tt.name); got != tt.want {
				t.Errorf("isExcludedRole(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// loadPolicy: file I/O
// ---------------------------------------------------------------------------

func TestLoadPolicy_FromFile(t *testing.T) {
	dir := t.TempDir()

	policyContent := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: file-role
rules:
  - resource: r1
    selfLink: https://example.com/r1
    permission: Read
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: ops
justification: "Ops team"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: ops-bind
subjects:
  - kind: Group
    name: ops
roleRef:
  kind: Role
  name: file-role
`
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(policyContent), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := "apiVersion: authzer/v1alpha1\nkind: Config\npolicy: policy.yaml\n"
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	p, err := loadPolicy()
	if err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	if len(p.Roles) != 1 {
		t.Errorf("expected 1 Role, got %d", len(p.Roles))
	}
	if len(p.Groups) != 1 {
		t.Errorf("expected 1 Group, got %d", len(p.Groups))
	}
	if len(p.RoleBindings) != 1 {
		t.Errorf("expected 1 RoleBinding, got %d", len(p.RoleBindings))
	}

	rules, justification, err := p.Resolve("ops")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if justification != "Ops team" {
		t.Errorf("justification = %q", justification)
	}
	if len(rules) != 1 || rules[0].Resource != "r1" {
		t.Errorf("unexpected rules: %+v", rules)
	}
}

func TestLoadPolicy_RelativePath(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "policies")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	policyContent := `---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: dev
justification: "Dev team"
`
	if err := os.WriteFile(filepath.Join(subdir, "rbac.yaml"), []byte(policyContent), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := "apiVersion: authzer/v1alpha1\nkind: Config\npolicy: policies/rbac.yaml\n"
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	p, err := loadPolicy()
	if err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	if _, ok := p.Groups["dev"]; !ok {
		t.Error("Group 'dev' not found after loading from relative subpath")
	}
}

func TestLoadPolicy_Missing(t *testing.T) {
	dir := t.TempDir()
	configContent := "apiVersion: authzer/v1alpha1\nkind: Config\npolicy: nonexistent.yaml\n"
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	_, err := loadPolicy()
	if err == nil {
		t.Fatal("expected error for missing policy file")
	}
}

func TestLoadPolicy_NoPolicyConfigured(t *testing.T) {
	dir := t.TempDir()
	configContent := "apiVersion: authzer/v1alpha1\nkind: Config\n"
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	p, err := loadPolicy()
	if err != nil {
		t.Fatalf("loadPolicy with no policy key: %v", err)
	}
	if len(p.Roles) != 0 || len(p.Groups) != 0 || len(p.RoleBindings) != 0 {
		t.Error("expected empty policy when no policy key configured")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
