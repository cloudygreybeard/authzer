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
	"strings"
	"time"
)

// ReconcileInput holds all the inputs needed to compute a
// reconciliation plan: the desired rules, the observed assignments,
// and configuration parameters.
type ReconcileInput struct {
	Rules       []Rule
	Assignments []Assignment
	NameByID    map[string]string
	RenewWithin time.Duration
	ExcludeList map[string]bool
	Targeted    []string
}

// ReconcileAction describes a single action to take (or skip) for a
// rule or undeclared assignment.
type ReconcileAction struct {
	Rule       Rule
	Assignment *Assignment
	Action     string // "renew", "request", "current", "excluded"
	Reason     string // human-readable explanation
}

// Reconcile computes the reconciliation plan by comparing desired
// rules against observed assignments. It returns the full list of
// actions sorted by type: renew, request, current, excluded.
//
// The logic handles:
//   - Rules matched to existing assignments: renew if expiring or
//     targeted, otherwise mark current.
//   - Rules with no matching assignment: mark as request.
//   - Undeclared assignments (not covered by any rule): renew if
//     expiring, otherwise mark current. Only included when no
//     specific targets are given.
func Reconcile(input ReconcileInput) []ReconcileAction {
	threshold := time.Now().Add(input.RenewWithin)
	targeted := len(input.Targeted) > 0

	isExcluded := func(name string) bool {
		return input.ExcludeList[strings.ToLower(name)]
	}

	assignmentByName := make(map[string]*Assignment, len(input.Assignments))
	assignmentByID := make(map[string]*Assignment, len(input.Assignments))
	for i := range input.Assignments {
		a := &input.Assignments[i]
		assignmentByName[a.Name] = a
		if a.ID != "" {
			assignmentByID[a.ID] = a
		}
	}

	var actions []ReconcileAction
	claimedNames := make(map[string]bool, len(input.Rules))

	for _, rule := range input.Rules {
		displayName := input.NameByID[rule.Resource]

		var a *Assignment
		if displayName != "" {
			a = assignmentByName[displayName]
		}
		if a == nil {
			a = assignmentByID[rule.Resource]
		}

		if a != nil {
			claimedNames[a.Name] = true

			if isExcluded(a.Name) {
				actions = append(actions, ReconcileAction{
					Rule:       rule,
					Assignment: a,
					Action:     "excluded",
					Reason:     "excluded by config",
				})
				continue
			}

			if targeted || isExpiringWithin(a.ExpirationDate, threshold) {
				actions = append(actions, ReconcileAction{
					Rule:       rule,
					Assignment: a,
					Action:     "renew",
					Reason:     "expiring",
				})
			} else {
				actions = append(actions, ReconcileAction{
					Rule:       rule,
					Assignment: a,
					Action:     "current",
					Reason:     "current",
				})
			}
		} else {
			if rule.SelfLink == "" {
				continue
			}
			actions = append(actions, ReconcileAction{
				Rule:   rule,
				Action: "request",
				Reason: "missing",
			})
		}
	}

	if !targeted {
		for i := range input.Assignments {
			a := &input.Assignments[i]
			if claimedNames[a.Name] {
				continue
			}

			if isExcluded(a.Name) {
				actions = append(actions, ReconcileAction{
					Assignment: a,
					Action:     "excluded",
					Reason:     "excluded, undeclared",
				})
				continue
			}

			if isExpiringWithin(a.ExpirationDate, threshold) {
				actions = append(actions, ReconcileAction{
					Assignment: a,
					Action:     "renew",
					Reason:     "expiring, undeclared",
				})
			} else {
				actions = append(actions, ReconcileAction{
					Assignment: a,
					Action:     "current",
					Reason:     "current, undeclared",
				})
			}
		}
	}

	return actions
}

// ActionableItems filters reconciliation actions to only those
// requiring execution (renew or request).
func ActionableItems(actions []ReconcileAction) []ReconcileAction {
	var out []ReconcileAction
	for _, a := range actions {
		if a.Action == "renew" || a.Action == "request" {
			out = append(out, a)
		}
	}
	return out
}

// CountByAction counts reconciliation actions grouped by action type.
func CountByAction(actions []ReconcileAction) map[string]int {
	counts := make(map[string]int)
	for _, a := range actions {
		counts[a.Action]++
	}
	return counts
}
