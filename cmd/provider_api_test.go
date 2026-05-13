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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func testBackend(baseURL string) *APIBackend {
	return &APIBackend{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "APIBackend"},
		Metadata: ObjectMeta{Name: "test-api"},
		Spec: APIBackendSpec{
			BaseURL: baseURL,
			Auth:    APIAuth{Method: "browser-cookies"},
			Identity: map[string]string{
				"domain":       "testdomain",
				"alias":        "testuser",
				"objectTypeId": "2",
			},
			Endpoints: APIEndpoints{
				List: APIEndpoint{
					Method:       "POST",
					Path:         "/api/User/memberships",
					BodyTemplate: `{"domain":"{{.Identity.domain}}","name":"{{.Identity.alias}}","objectTypeId":{{.Identity.objectTypeId}}}`,
					FieldMap: map[string]string{
						"id":             "EntitlementName",
						"name":           "EntitlementDisplayName",
						"role":           "PermissionName",
						"expirationDate": "ExpirationDate",
					},
				},
				Validate: APIEndpoint{
					Method:       "POST",
					Path:         "/api/Entitlement/validate/members",
					BodyTemplate: `{"entitlementName":"{{.Entitlement}}","addMembers":[{"account":"{{.Identity.alias}}","accountDomain":"{{.Identity.domain}}","friendlyPermissionName":"{{.Permission}}"}]}`,
				},
				Submit: APIEndpoint{
					Method:       "POST",
					Path:         "/api/Entitlement/modify",
					BodyTemplate: `{"name":"{{.Entitlement}}","domain":"entitlement","businessJustification":"{{.Justification}}","addEntitlementMembers":[{"account":"{{.Identity.alias}}","accountDomain":"{{.Identity.domain}}","friendlyPermissionName":"{{.Permission}}"}]}`,
				},
			},
		},
	}
}

func TestRenderBody(t *testing.T) {
	tmpl := `{"domain":"{{.Identity.domain}}","name":"{{.Identity.alias}}"}`
	data := templateData{
		Identity: map[string]string{"domain": "testdomain", "alias": "jdoe"},
	}

	result, err := renderBody(tmpl, data)
	if err != nil {
		t.Fatalf("renderBody: %v", err)
	}

	expected := `{"domain":"testdomain","name":"jdoe"}`
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestRenderBodyWithRuntimeVars(t *testing.T) {
	tmpl := `{"entitlement":"{{.Entitlement}}","permission":"{{.Permission}}","justification":"{{.Justification}}"}`
	data := templateData{
		Identity:      map[string]string{"alias": "jdoe"},
		Entitlement:   "my-entitlement",
		Permission:    "ReadWrite",
		Justification: "SRE team member",
	}

	result, err := renderBody(tmpl, data)
	if err != nil {
		t.Fatalf("renderBody: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, result)
	}
	if parsed["entitlement"] != "my-entitlement" {
		t.Errorf("entitlement = %q, want %q", parsed["entitlement"], "my-entitlement")
	}
	if parsed["justification"] != "SRE team member" {
		t.Errorf("justification = %q, want %q", parsed["justification"], "SRE team member")
	}
}

func TestRenderBodyEmpty(t *testing.T) {
	result, err := renderBody("", templateData{})
	if err != nil {
		t.Fatalf("renderBody empty: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestMapAssignment(t *testing.T) {
	raw := map[string]interface{}{
		"EntitlementName":        "my-ent",
		"EntitlementDisplayName": "My Entitlement",
		"PermissionName":         "ReadWrite",
		"ExpirationDate":         "2026-07-01T00:00:00Z",
	}
	fieldMap := map[string]string{
		"id":             "EntitlementName",
		"name":           "EntitlementDisplayName",
		"role":           "PermissionName",
		"expirationDate": "ExpirationDate",
	}

	a := mapAssignment(raw, fieldMap)

	if a.ID != "my-ent" {
		t.Errorf("ID = %q, want %q", a.ID, "my-ent")
	}
	if a.Name != "My Entitlement" {
		t.Errorf("Name = %q, want %q", a.Name, "My Entitlement")
	}
	if a.Role != "ReadWrite" {
		t.Errorf("Role = %q, want %q", a.Role, "ReadWrite")
	}
	if a.ExpirationDate != "2026-07-01T00:00:00Z" {
		t.Errorf("ExpirationDate = %q, want %q", a.ExpirationDate, "2026-07-01T00:00:00Z")
	}
}

func TestMapAssignmentNullValues(t *testing.T) {
	raw := map[string]interface{}{
		"EntitlementName": "ent-1",
		"ExpirationDate":  nil,
	}
	fieldMap := map[string]string{
		"id":             "EntitlementName",
		"expirationDate": "ExpirationDate",
	}

	a := mapAssignment(raw, fieldMap)
	if a.ID != "ent-1" {
		t.Errorf("ID = %q, want %q", a.ID, "ent-1")
	}
	if a.ExpirationDate != "" {
		t.Errorf("ExpirationDate = %q, want empty for null", a.ExpirationDate)
	}
}

func TestAPIProviderList(t *testing.T) {
	memberships := []map[string]interface{}{
		{
			"EntitlementName":        "ent-a",
			"EntitlementDisplayName": "Entitlement A",
			"PermissionName":         "ReadOnly",
			"ExpirationDate":         "2026-08-01T00:00:00Z",
		},
		{
			"EntitlementName":        "ent-b",
			"EntitlementDisplayName": "Entitlement B",
			"PermissionName":         "ReadWrite",
			"ExpirationDate":         nil,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/User/memberships" {
			http.NotFound(w, r)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}
		if req["domain"] != "testdomain" || req["name"] != "testuser" {
			t.Errorf("unexpected request body: %s", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(memberships)
	}))
	defer srv.Close()

	backend := testBackend(srv.URL)
	provider := newAPIProviderHTTP(backend, srv.Client(), nil)

	assignments, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(assignments))
	}

	if assignments[0].ID != "ent-a" {
		t.Errorf("assignments[0].ID = %q, want %q", assignments[0].ID, "ent-a")
	}
	if assignments[0].Name != "Entitlement A" {
		t.Errorf("assignments[0].Name = %q", assignments[0].Name)
	}
	if assignments[1].ExpirationDate != "" {
		t.Errorf("assignments[1].ExpirationDate = %q, want empty for null", assignments[1].ExpirationDate)
	}
}

func TestAPIProviderListCaching(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"EntitlementName": "ent-1", "EntitlementDisplayName": "E1", "PermissionName": "Read"},
		})
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)
	ctx := context.Background()

	if _, err := provider.List(ctx); err != nil {
		t.Fatalf("List 1: %v", err)
	}
	if _, err := provider.List(ctx); err != nil {
		t.Fatalf("List 2: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", calls)
	}

	provider.InvalidateCache()
	if _, err := provider.List(ctx); err != nil {
		t.Fatalf("List 3: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls after invalidation, got %d", calls)
	}
}

func TestAPIProviderCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"EntitlementName": "ent-a", "EntitlementDisplayName": "Entitlement A", "PermissionName": "Read"},
			{"EntitlementName": "ent-b", "EntitlementDisplayName": "Entitlement B", "PermissionName": "Write"},
		})
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)
	ctx := context.Background()

	result, err := provider.Check(ctx, Rule{Resource: "ent-a", Permission: "Read"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Satisfied {
		t.Error("expected satisfied for ent-a")
	}

	result, err = provider.Check(ctx, Rule{Resource: "ent-missing", Permission: "Read"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Satisfied {
		t.Error("expected not satisfied for missing resource")
	}
}

func TestAPIProviderApplyDryRun(t *testing.T) {
	validateCalled := false
	submitCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/Entitlement/validate/members":
			validateCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ValidMembers":   []interface{}{map[string]interface{}{"Name": "testuser", "IsRenewal": true}},
				"InvalidMembers": []interface{}{},
				"Errors":         []interface{}{},
			})
		case "/api/Entitlement/modify":
			submitCalled = true
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)

	result, err := provider.Apply(context.Background(),
		Rule{Resource: "ent-a", Permission: "ReadWrite"},
		"test justification", true)
	if err != nil {
		t.Fatalf("Apply dry-run: %v", err)
	}

	if !validateCalled {
		t.Error("expected validate endpoint to be called")
	}
	if submitCalled {
		t.Error("submit endpoint should not be called in dry-run mode")
	}
	if result.Action != "validated" {
		t.Errorf("action = %q, want %q", result.Action, "validated")
	}
}

func TestAPIProviderApplySubmit(t *testing.T) {
	var submittedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/Entitlement/validate/members":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ValidMembers":   []interface{}{map[string]interface{}{"Name": "testuser"}},
				"InvalidMembers": []interface{}{},
				"Errors":         []interface{}{},
			})
		case "/api/Entitlement/modify":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &submittedBody)
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)

	result, err := provider.Apply(context.Background(),
		Rule{Resource: "ent-a", Permission: "ReadWrite"},
		"SRE renewal", false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.Action != "renewed" {
		t.Errorf("action = %q, want %q", result.Action, "renewed")
	}
	if submittedBody == nil {
		t.Fatal("submit body was nil")
	}
	if submittedBody["name"] != "ent-a" {
		t.Errorf("submitted name = %v, want %q", submittedBody["name"], "ent-a")
	}
	if submittedBody["businessJustification"] != "SRE renewal" {
		t.Errorf("submitted justification = %v", submittedBody["businessJustification"])
	}
}

func TestAPIProviderApplyValidationFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/Entitlement/validate/members" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ValidMembers":   []interface{}{},
				"InvalidMembers": []interface{}{map[string]interface{}{"Name": "testuser", "Reason": "blocked"}},
				"Errors":         []interface{}{},
			})
			return
		}
		t.Error("submit should not be called after validation failure")
		http.NotFound(w, r)
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)

	result, err := provider.Apply(context.Background(),
		Rule{Resource: "ent-a", Permission: "ReadWrite"},
		"justification", false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Action != "failed" {
		t.Errorf("action = %q, want %q", result.Action, "failed")
	}
}

func TestAPIProviderKindsAndCapabilities(t *testing.T) {
	p := newAPIProviderHTTP(&APIBackend{}, &http.Client{}, nil)
	if p.Name() != "api" {
		t.Errorf("Name = %q", p.Name())
	}
	kinds := p.Kinds()
	if len(kinds) != 1 || kinds[0] != "Entitlement" {
		t.Errorf("Kinds = %v, want [Entitlement]", kinds)
	}
	if p.Capabilities() != CapList|CapCheck|CapApply {
		t.Errorf("unexpected capabilities: %v", p.Capabilities())
	}

	p2 := newAPIProviderHTTP(&APIBackend{}, &http.Client{}, []string{"Custom", "Other"})
	kinds2 := p2.Kinds()
	if len(kinds2) != 2 || kinds2[0] != "Custom" {
		t.Errorf("custom Kinds = %v", kinds2)
	}
}

func TestAPIProviderListAssignments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"EntitlementName":        "ent-x",
				"EntitlementDisplayName": "Entitlement X",
				"PermissionName":         "ReadOnly",
				"ExpirationDate":         "2026-09-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	provider := newAPIProviderHTTP(testBackend(srv.URL), srv.Client(), nil)
	assignments, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].ID != "ent-x" {
		t.Errorf("ID = %q", assignments[0].ID)
	}
	if assignments[0].Name != "Entitlement X" {
		t.Errorf("Name = %q", assignments[0].Name)
	}
	if assignments[0].Kind != "Entitlement" {
		t.Errorf("Kind = %q, want Entitlement", assignments[0].Kind)
	}
}

func TestLoadAPIBackend(t *testing.T) {
	dir := t.TempDir()
	ab := &APIBackend{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "APIBackend"},
		Metadata: ObjectMeta{Name: "test"},
		Spec: APIBackendSpec{
			BaseURL: "https://api.example.com",
			Auth:    APIAuth{Method: "browser-cookies"},
			Identity: map[string]string{
				"domain": "test",
				"alias":  "user",
			},
			Endpoints: APIEndpoints{
				List: APIEndpoint{
					Method: "POST",
					Path:   "/api/list",
				},
				Submit: APIEndpoint{
					Method: "POST",
					Path:   "/api/submit",
				},
			},
		},
	}

	data, err := yaml.Marshal(ab)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api-backend.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
