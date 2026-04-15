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
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPrintPolicyStructured_YAML(t *testing.T) {
	policy := buildTestPolicy()
	err := printPolicyStructuredToBuffer(policy, "yaml")
	if err != nil {
		t.Fatalf("printPolicyStructured(yaml): %v", err)
	}
}

func TestPrintPolicyStructured_JSON(t *testing.T) {
	policy := buildTestPolicy()
	err := printPolicyStructuredToBuffer(policy, "json")
	if err != nil {
		t.Fatalf("printPolicyStructured(json): %v", err)
	}
}

func TestPrintPolicyStructured_InvalidFormat(t *testing.T) {
	policy := buildTestPolicy()
	err := printPolicyStructuredToBuffer(policy, "xml")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestPolicyDataMarshal_YAML(t *testing.T) {
	policy := buildTestPolicy()
	data := buildPolicyData(policy)

	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "Policy",
		Data:       data,
	}

	out, err := yaml.Marshal(&envelope)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	s := string(out)

	if len(s) == 0 {
		t.Fatal("yaml output is empty")
	}

	var roundTrip OutputEnvelope
	if err := yaml.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	if roundTrip.Kind != "Policy" {
		t.Errorf("kind = %q, want Policy", roundTrip.Kind)
	}
}

func TestPolicyDataMarshal_JSON(t *testing.T) {
	policy := buildTestPolicy()
	data := buildPolicyData(policy)

	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "Policy",
		Data:       data,
	}

	out, err := json.MarshalIndent(&envelope, "", "  ")
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	var roundTrip map[string]any
	if err := json.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if roundTrip["kind"] != "Policy" {
		t.Errorf("kind = %v, want Policy", roundTrip["kind"])
	}
}

func buildTestPolicy() *Policy {
	return &Policy{
		Groups: map[string]*Group{
			"sre": {
				Metadata:      ObjectMeta{Name: "sre"},
				Justification: "SRE team member",
			},
		},
		Roles: map[string]*Role{
			"infra-access": {
				Metadata: ObjectMeta{Name: "infra-access"},
				Rules: []Rule{
					{Kind: "Entitlement", Resource: "storage-001", Permission: "ReadOnly"},
					{Kind: "Entitlement", Resource: "compute-002", Permission: "ReadWrite"},
				},
			},
		},
		RoleBindings: []*RoleBinding{
			{
				Metadata: ObjectMeta{Name: "sre-infra"},
				Subjects: []Subject{{Kind: "Group", Name: "sre"}},
				RoleRef:  RoleRef{Kind: "Role", Name: "infra-access"},
			},
		},
	}
}

func buildPolicyData(policy *Policy) policyData {
	data := policyData{}
	for _, g := range policy.Groups {
		data.Groups = append(data.Groups, *g)
	}
	for _, r := range policy.Roles {
		data.Roles = append(data.Roles, *r)
	}
	for _, rb := range policy.RoleBindings {
		data.RoleBindings = append(data.RoleBindings, *rb)
	}
	return data
}

func printPolicyStructuredToBuffer(policy *Policy, format string) error {
	data := buildPolicyData(policy)
	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "Policy",
		Data:       data,
	}

	switch format {
	case "yaml":
		_, err := yaml.Marshal(&envelope)
		return err
	case "json":
		_, err := json.MarshalIndent(&envelope, "", "  ")
		return err
	default:
		return printPolicyStructured(policy, format)
	}
}
