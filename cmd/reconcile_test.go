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
	"testing"
	"time"
)

func TestReconcile_AllCurrent(t *testing.T) {
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "ent-a", SelfLink: "/a", Permission: "Read"},
		},
		Assignments: []Assignment{
			{ID: "ent-a", Name: "Entitlement A", ExpirationDate: "2027-01-01"},
		},
		NameByID:    map[string]string{"ent-a": "Entitlement A"},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "current" {
		t.Errorf("action = %q, want %q", actions[0].Action, "current")
	}
}

func TestReconcile_ExpiringRenewal(t *testing.T) {
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "ent-a", SelfLink: "/a", Permission: "Read"},
		},
		Assignments: []Assignment{
			{ID: "ent-a", Name: "Entitlement A", ExpirationDate: tomorrow},
		},
		NameByID:    map[string]string{"ent-a": "Entitlement A"},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "renew" {
		t.Errorf("action = %q, want %q", actions[0].Action, "renew")
	}
}

func TestReconcile_MissingRequest(t *testing.T) {
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "ent-missing", SelfLink: "/missing", Permission: "Read"},
		},
		Assignments: []Assignment{},
		NameByID:    map[string]string{},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "request" {
		t.Errorf("action = %q, want %q", actions[0].Action, "request")
	}
}

func TestReconcile_Excluded(t *testing.T) {
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "ent-a", SelfLink: "/a", Permission: "Read"},
		},
		Assignments: []Assignment{
			{ID: "ent-a", Name: "Entitlement A", ExpirationDate: "2027-01-01"},
		},
		NameByID:    map[string]string{"ent-a": "Entitlement A"},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{"entitlement a": true},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "excluded" {
		t.Errorf("action = %q, want %q", actions[0].Action, "excluded")
	}
}

func TestReconcile_UndeclaredExpiring(t *testing.T) {
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	input := ReconcileInput{
		Rules: []Rule{},
		Assignments: []Assignment{
			{ID: "ent-extra", Name: "Extra", ExpirationDate: tomorrow},
		},
		NameByID:    map[string]string{},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "renew" {
		t.Errorf("action = %q, want %q", actions[0].Action, "renew")
	}
	if actions[0].Reason != "expiring, undeclared" {
		t.Errorf("reason = %q, want %q", actions[0].Reason, "expiring, undeclared")
	}
}

func TestReconcile_TargetedForcesRenewal(t *testing.T) {
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "ent-a", SelfLink: "/a", Permission: "Read"},
		},
		Assignments: []Assignment{
			{ID: "ent-a", Name: "Entitlement A", ExpirationDate: "2027-01-01"},
		},
		NameByID:    map[string]string{"ent-a": "Entitlement A"},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
		Targeted:    []string{"Entitlement A"},
	}

	actions := Reconcile(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Action != "renew" {
		t.Errorf("action = %q, want %q (targeted should force renewal)", actions[0].Action, "renew")
	}
}

func TestActionableItems(t *testing.T) {
	actions := []ReconcileAction{
		{Action: "renew"},
		{Action: "current"},
		{Action: "request"},
		{Action: "excluded"},
	}

	actionable := ActionableItems(actions)
	if len(actionable) != 2 {
		t.Fatalf("expected 2 actionable, got %d", len(actionable))
	}
	if actionable[0].Action != "renew" {
		t.Errorf("[0] = %q", actionable[0].Action)
	}
	if actionable[1].Action != "request" {
		t.Errorf("[1] = %q", actionable[1].Action)
	}
}

func TestCountByAction(t *testing.T) {
	actions := []ReconcileAction{
		{Action: "renew"},
		{Action: "renew"},
		{Action: "current"},
		{Action: "request"},
		{Action: "excluded"},
	}

	counts := CountByAction(actions)
	if counts["renew"] != 2 {
		t.Errorf("renew = %d, want 2", counts["renew"])
	}
	if counts["current"] != 1 {
		t.Errorf("current = %d, want 1", counts["current"])
	}
	if counts["request"] != 1 {
		t.Errorf("request = %d, want 1", counts["request"])
	}
}

func TestReconcile_NoSelfLinkSkipped(t *testing.T) {
	input := ReconcileInput{
		Rules: []Rule{
			{Resource: "no-link", Permission: "Read"},
		},
		Assignments: []Assignment{},
		NameByID:    map[string]string{},
		RenewWithin: 30 * 24 * time.Hour,
		ExcludeList: map[string]bool{},
	}

	actions := Reconcile(input)
	if len(actions) != 0 {
		t.Errorf("expected 0 actions (no selfLink), got %d", len(actions))
	}
}
