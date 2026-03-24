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
	"flag"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

func TestGolden_InspectResult(t *testing.T) {
	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "InspectResult",
		Data: InspectData{
			Updated:     "2026-04-08T12:00:00Z",
			CDPEndpoint: "http://127.0.0.1:9222",
			TotalItems:  1,
			Items: []Resource{
				{
					Kind:        "Entitlement",
					ID:          "test-res-1",
					SelfLink:    "https://example.com/res/test-res-1",
					Name:        "Test Resource One",
					Status:      "Active",
					Domains:     []string{"example.com"},
					Description: "A test resource for golden file validation.",
					RequestForm: &RequestForm{
						Account:        "user@example.com",
						AccountOptions: []string{"user@example.com"},
						Permissions: []FormOption{
							{Name: "ReadOnly", Selected: true},
							{Name: "ReadWrite", Selected: false},
						},
						HasTermsCheckbox:   true,
						TermsCheckboxLabel: "I agree",
						HasJustification:   true,
					},
				},
			},
		},
	}

	goldenPath := filepath.Join("testdata", "inspect-result.golden.yaml")
	got, err := yaml.Marshal(&envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if *update {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		t.Log("updated golden file")
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("output differs from golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, want)
	}
}

func TestGolden_PolicyRoundTrip(t *testing.T) {
	policyYAML := `---
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: test-access
  labels:
    team: test
  annotations:
    description: "Test role for golden file"
rules:
  - kind: Entitlement
    resource: res-1
    selfLink: https://example.com/res-1
    permission: ReadOnly
  - kind: Entitlement
    resource: res-2
    selfLink: https://example.com/res-2
    permission: ReadWrite
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: testers
justification: "QA team member"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: testers-access
subjects:
  - kind: Group
    name: testers
roleRef:
  kind: Role
  name: test-access
`

	p, err := parsePolicy([]byte(policyYAML))
	if err != nil {
		t.Fatalf("parsePolicy: %v", err)
	}

	if len(p.Roles) != 1 {
		t.Fatalf("expected 1 Role, got %d", len(p.Roles))
	}
	role := p.Roles["test-access"]
	if role == nil {
		t.Fatal("Role 'test-access' not found")
	}

	reMarshalled, err := yaml.Marshal(role)
	if err != nil {
		t.Fatalf("marshal Role: %v", err)
	}

	goldenPath := filepath.Join("testdata", "policy-role-roundtrip.golden.yaml")

	if *update {
		if err := os.WriteFile(goldenPath, reMarshalled, 0644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		t.Log("updated golden file")
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(reMarshalled) != string(want) {
		t.Errorf("output differs from golden file %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, reMarshalled, want)
	}

	// Verify the round-trip preserves semantic content.
	var roundTripped Role
	if err := yaml.Unmarshal(reMarshalled, &roundTripped); err != nil {
		t.Fatalf("unmarshal round-tripped Role: %v", err)
	}
	if roundTripped.Metadata.Name != "test-access" {
		t.Errorf("round-trip name = %q", roundTripped.Metadata.Name)
	}
	if len(roundTripped.Rules) != 2 {
		t.Errorf("round-trip rules count = %d, want 2", len(roundTripped.Rules))
	}
	if roundTripped.Metadata.Labels["team"] != "test" {
		t.Errorf("round-trip label team = %q", roundTripped.Metadata.Labels["team"])
	}
}
