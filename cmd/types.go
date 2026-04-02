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

import "gopkg.in/yaml.v3"

// APIVersion is the schema version for config and output documents.
const APIVersion = "authzer/v1alpha1"

// ---------------------------------------------------------------------------
// Kubernetes-aligned resource meta types
// ---------------------------------------------------------------------------

// TypeMeta identifies a resource's API version and kind.
type TypeMeta struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// ObjectMeta holds standard resource metadata.
type ObjectMeta struct {
	Name        string            `yaml:"name"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// ---------------------------------------------------------------------------
// RBAC resource types
// ---------------------------------------------------------------------------

// Role defines access rules for portal-managed resources. When
// AggregationRule is set, Rules are computed at load time by selecting
// other Roles by label and merging their rules.
type Role struct {
	TypeMeta        `yaml:",inline"`
	Metadata        ObjectMeta       `yaml:"metadata"`
	Rules           []Rule           `yaml:"rules,omitempty"`
	AggregationRule *AggregationRule `yaml:"aggregationRule,omitempty"`
}

// Rule is a single access grant within a Role. Kind carries the
// site-specific resource type (e.g. "Entitlement"); Resource is the
// identifier; Permission is the desired access level.
type Rule struct {
	Kind       string `yaml:"kind,omitempty"`
	Resource   string `yaml:"resource"`
	SelfLink   string `yaml:"selfLink"`
	Permission string `yaml:"permission"`
}

// AggregationRule selects other Roles by label to merge their rules.
type AggregationRule struct {
	RoleSelectors []LabelSelector `yaml:"roleSelectors"`
}

// LabelSelector matches resources by label key-value pairs.
type LabelSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels"`
}

// RoleBinding binds subjects to a role reference.
type RoleBinding struct {
	TypeMeta `yaml:",inline"`
	Metadata ObjectMeta `yaml:"metadata"`
	Subjects []Subject  `yaml:"subjects"`
	RoleRef  RoleRef    `yaml:"roleRef"`
}

// Subject identifies who receives access.
type Subject struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// RoleRef references a Role by kind and name.
type RoleRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// Group represents an organisational identity with justification text.
type Group struct {
	TypeMeta      `yaml:",inline"`
	Metadata      ObjectMeta `yaml:"metadata"`
	Justification string     `yaml:"justification"`
}

// ResourceList wraps a heterogeneous collection of typed resources,
// used as a client-side transport format (analogous to kubectl's
// kind: List).
type ResourceList struct {
	TypeMeta `yaml:",inline"`
	Items    []yaml.Node `yaml:"items"`
}

// Policy holds the resolved set of RBAC resources loaded from the
// policy file. RoleBindings is a slice to preserve document order,
// which matters for last-write-wins deduplication of overlapping rules.
type Policy struct {
	Roles        map[string]*Role
	Groups       map[string]*Group
	RoleBindings []*RoleBinding
}

// ---------------------------------------------------------------------------
// CLI output envelope
// ---------------------------------------------------------------------------

// OutputEnvelope wraps every CLI output document.
type OutputEnvelope struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Data       any    `yaml:"data" json:"data"`
}

// ---------------------------------------------------------------------------
// Scraped portal data (generic — site-specific kind carried as a value)
// ---------------------------------------------------------------------------

// Resource holds all information extracted from a portal resource page
// and its request form dialog. This is the "deep" metadata collected
// when visiting individual entitlement pages.
type Resource struct {
	Kind                string       `yaml:"kind,omitempty" json:"kind,omitempty"`
	ID                  string       `yaml:"id" json:"id"`
	SelfLink            string       `yaml:"selfLink" json:"selfLink"`
	Name                string       `yaml:"name" json:"name"`
	Managed             *bool        `yaml:"managed,omitempty" json:"managed,omitempty"`
	Status              string       `yaml:"status" json:"status"`
	Domains             []string     `yaml:"domains" json:"domains"`
	Description         string       `yaml:"description" json:"description"`
	PrimaryOwners       []string     `yaml:"primaryOwners" json:"primaryOwners"`
	SecondaryOwners     []string     `yaml:"secondaryOwners" json:"secondaryOwners"`
	CustomJustification *string      `yaml:"customJustification,omitempty" json:"customJustification,omitempty"`
	TermsAndConditions  *string      `yaml:"termsAndConditions,omitempty" json:"termsAndConditions,omitempty"`
	RequestForm         *RequestForm `yaml:"requestForm,omitempty" json:"requestForm,omitempty"`
	Error               string       `yaml:"error,omitempty" json:"error,omitempty"`
}

// RequestForm holds the fields discovered inside the request dialog.
type RequestForm struct {
	Account            string       `yaml:"account" json:"account"`
	AccountOptions     []string     `yaml:"accountOptions" json:"accountOptions"`
	Permissions        []FormOption `yaml:"permissions" json:"permissions"`
	HasTermsCheckbox   bool         `yaml:"hasTermsCheckbox" json:"hasTermsCheckbox"`
	TermsCheckboxLabel string       `yaml:"termsCheckboxLabel,omitempty" json:"termsCheckboxLabel,omitempty"`
	TermsText          string       `yaml:"termsText,omitempty" json:"termsText,omitempty"`
	HasJustification   bool         `yaml:"hasJustificationField" json:"hasJustificationField"`
}

// FormOption is a single option presented in the portal's request form
// (e.g. a radio button for permission level).
type FormOption struct {
	Name     string `yaml:"name" json:"name"`
	Selected bool   `yaml:"selected" json:"selected"`
}

// ---------------------------------------------------------------------------
// Membership data from the My Memberships table
// ---------------------------------------------------------------------------

// Membership represents a current membership scraped from the portal's
// memberships table. This is the lightweight data returned by "get".
type Membership struct {
	Kind           string `yaml:"kind,omitempty" json:"kind,omitempty"`
	ID             string `yaml:"id" json:"id"`
	Name           string `yaml:"name" json:"name"`
	SelfLink       string `yaml:"selfLink,omitempty" json:"selfLink,omitempty"`
	Account        string `yaml:"account" json:"account"`
	Role           string `yaml:"role" json:"role"`
	ExpirationDate string `yaml:"expirationDate" json:"expirationDate"`
	Expiring       bool   `yaml:"expiring" json:"expiring"`
}

// ---------------------------------------------------------------------------
// Command output data types
// ---------------------------------------------------------------------------

// GetData is the data payload for the "get" command. It combines
// lightweight membership data with optional deep resource metadata
// loaded from cache.
type GetData struct {
	Updated     string       `yaml:"updated" json:"updated"`
	CDPEndpoint string       `yaml:"cdpEndpoint" json:"cdpEndpoint"`
	TotalItems  int          `yaml:"totalItems" json:"totalItems"`
	Items       []Membership `yaml:"items" json:"items"`
	Details     []Resource   `yaml:"details,omitempty" json:"details,omitempty"`
}

// InspectData is the data payload for deep resource survey output.
// Retained for cache file compatibility.
type InspectData struct {
	Updated     string     `yaml:"updated" json:"updated"`
	CDPEndpoint string     `yaml:"cdpEndpoint" json:"cdpEndpoint"`
	TotalItems  int        `yaml:"totalItems" json:"totalItems"`
	Items       []Resource `yaml:"items" json:"items"`
}

// ApplyData is the data payload for the "apply" command output.
type ApplyData struct {
	Updated       string      `yaml:"updated" json:"updated"`
	Group         string      `yaml:"group" json:"group"`
	Justification string      `yaml:"justification" json:"justification"`
	DryRun        string      `yaml:"dryRun" json:"dryRun"`
	TotalItems    int         `yaml:"totalItems" json:"totalItems"`
	Summary       ApplySummary `yaml:"summary" json:"summary"`
	Items         []Action    `yaml:"items" json:"items"`
}

// ApplySummary counts actions by type.
type ApplySummary struct {
	Renew   int `yaml:"renew" json:"renew"`
	Request int `yaml:"request" json:"request"`
	Current int `yaml:"current" json:"current"`
	Failed  int `yaml:"failed" json:"failed"`
}

// Action represents a planned or executed reconciliation action.
type Action struct {
	Kind        string `yaml:"kind,omitempty" json:"kind,omitempty"`
	ID          string `yaml:"id" json:"id"`
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	Action      string `yaml:"action" json:"action"`
	Reason      string `yaml:"reason" json:"reason"`
	SelfLink    string `yaml:"selfLink,omitempty" json:"selfLink,omitempty"`
	CurrentRole string `yaml:"currentRole,omitempty" json:"currentRole,omitempty"`
	DesiredRole string `yaml:"desiredRole" json:"desiredRole"`
	Error       string `yaml:"error,omitempty" json:"error,omitempty"`
}
