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

// mock-portal serves a minimal entitlements portal that is compatible
// with authzer's site-pack scripts and selectors. It provides enough
// DOM structure and interactive behaviour for authzer to exercise its
// full CDP workflow: list memberships, select checkboxes, open renew
// dialogs, fill justification, accept terms, and submit.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := ":8080"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	} else if v := os.Getenv("MOCK_PORTAL_ADDR"); v != "" {
		addr = v
	}

	initEntitlements()

	mux := http.NewServeMux()
	mux.HandleFunc("/portal/memberships", membershipsPage)
	mux.HandleFunc("/portal/access/", detailPage)
	mux.HandleFunc("/api/renew", handleRenew)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/portal/memberships", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	log.Printf("mock-portal listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "mock-portal: %v\n", err)
		os.Exit(1)
	}
}

func handleRenew(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok","message":"Membership renewed successfully."}`)
}

type entitlement struct {
	Name        string
	ID          string
	Role        string
	Expiry      time.Time
	Account     string
	HasTerms    bool
	TermsText   string
	Description string
}

var entitlements []entitlement

func initEntitlements() {
	now := time.Now().UTC()
	entitlements = []entitlement{
		{
			Name:    "Cloud Storage Access",
			ID:      "cloud-storage-a1b2",
			Role:    "ReadOnly",
			Expiry:  now.AddDate(0, 0, 30),
			Account: "demo/user",
			Description: "Provides read-only access to cloud storage accounts " +
				"for monitoring and audit purposes.",
		},
		{
			Name:    "API Gateway Admin",
			ID:      "api-gateway-c3d4",
			Role:    "ReadWrite",
			Expiry:  now.AddDate(0, 0, 25),
			Account: "demo/user",
			Description: "Administrative access to API gateway configuration " +
				"including route management and policy enforcement.",
		},
		{
			Name:     "Log Analytics Reader",
			ID:       "log-analytics-e5f6",
			Role:     "ReadOnly",
			Expiry:   now.AddDate(0, 0, 20),
			Account:  "demo/user",
			HasTerms: true,
			TermsText: "I acknowledge and agree that I have read these approval " +
				"instructions carefully: This entitlement will be approved by " +
				"your manager. Please note that, in most instances, it will be " +
				"the only approval needed for your access request.",
			Description: "Read access to centralised log analytics workspace " +
				"for operational monitoring and incident investigation.",
		},
		{
			Name:    "Container Registry Push",
			ID:      "container-reg-g7h8",
			Role:    "ReadWrite",
			Expiry:  now.AddDate(0, 0, 90),
			Account: "demo/user",
			Description: "Push access to the shared container image registry " +
				"for CI/CD pipeline artifact storage.",
		},
		{
			Name:    "Service Bus Contributor",
			ID:      "service-bus-i9j0",
			Role:    "ReadWrite",
			Expiry:  now.AddDate(0, 0, 120),
			Account: "demo/user",
			Description: "Contributor access to service bus namespaces for " +
				"message queue configuration and topic management.",
		},
		{
			Name:     "Key Vault Secrets Read",
			ID:       "key-vault-k1l2",
			Role:     "ReadOnly",
			Expiry:   now.AddDate(0, 0, 15),
			Account:  "demo/user",
			HasTerms: true,
			TermsText: "You acknowledge and agree that your access will be " +
				"approved by the Secondary Owner(s) listed in the entitlement. " +
				"By accessing this system, you acknowledge and agree to the " +
				"following terms: Authorized Use Only. All data is " +
				"confidential and subject to company policies and applicable " +
				"regulations.",
			Description: "Read access to key vault secrets for application " +
				"configuration and credential retrieval at runtime.",
		},
	}
}

func findEntitlement(id string) *entitlement {
	id = strings.ToLower(id)
	for i := range entitlements {
		if strings.ToLower(entitlements[i].ID) == id {
			return &entitlements[i]
		}
	}
	return nil
}
