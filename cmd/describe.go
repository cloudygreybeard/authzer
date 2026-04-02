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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var describeCmd = &cobra.Command{
	Use:   "describe [RESOURCE...]",
	Short: "Show detailed information about resources",
	Long: `Display a human-readable summary of one or more resources, combining
deep metadata from cached survey data with current membership status.

Without arguments, all cached resources are described. With arguments,
only matching resources are shown (matched by ID or name, case-insensitive).

Data comes from the local cache populated by "authzer get --refresh".
If no cache exists, run "authzer get --refresh" first.`,
	RunE: runDescribe,
}

func init() {
	rootCmd.AddCommand(describeCmd)
}

func runDescribe(cmd *cobra.Command, args []string) error {
	cacheDir := cacheDirectory()
	detailsPath := filepath.Join(cacheDir, "details-cache.yaml")
	membershipsPath := filepath.Join(cacheDir, "memberships-cache.yaml")

	details, err := readCache(detailsPath)
	if err != nil || len(details) == 0 {
		return fmt.Errorf("no cached resource details; run 'authzer get --refresh' first")
	}

	var memberships []Membership
	if data, err := os.ReadFile(membershipsPath); err == nil {
		_ = yaml.Unmarshal(data, &memberships)
	}
	membershipByID := make(map[string]*Membership, len(memberships))
	for i := range memberships {
		membershipByID[memberships[i].ID] = &memberships[i]
	}

	if len(args) > 0 {
		details = filterDetails(details, args)
	}

	if len(details) == 0 {
		fmt.Fprintf(os.Stderr, "No matching resources found.\n")
		return nil
	}

	w := os.Stdout
	for i, res := range details {
		if i > 0 {
			fmt.Fprintln(w)
		}
		printResource(w, res, membershipByID[res.ID])
	}
	return nil
}

func filterDetails(details []Resource, args []string) []Resource {
	var out []Resource
	for _, d := range details {
		for _, arg := range args {
			if strings.EqualFold(d.ID, arg) || strings.EqualFold(d.Name, arg) {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

func printResource(w io.Writer, r Resource, m *Membership) {
	field := func(label, value string) {
		if value != "" {
			fmt.Fprintf(w, "%-20s%s\n", label+":", value)
		}
	}
	fieldList := func(label string, values []string) {
		if len(values) == 0 {
			fmt.Fprintf(w, "%-20s<none>\n", label+":")
			return
		}
		for i, v := range values {
			if i == 0 {
				fmt.Fprintf(w, "%-20s%s\n", label+":", v)
			} else {
				fmt.Fprintf(w, "%-20s%s\n", "", v)
			}
		}
	}

	field("Name", r.Name)
	field("ID", r.ID)
	field("Kind", r.Kind)
	field("Status", r.Status)
	field("Link", r.SelfLink)
	if r.Managed != nil {
		if *r.Managed {
			field("Managed", "yes (declared in policy)")
		} else {
			field("Managed", "no (not declared in policy)")
		}
	}

	if len(r.Domains) > 0 {
		field("Domains", strings.Join(r.Domains, ", "))
	}

	if r.Description != "" {
		fmt.Fprintf(w, "%-20s%s\n", "Description:", r.Description)
	}

	fmt.Fprintln(w)
	fieldList("Primary Owners", r.PrimaryOwners)
	fieldList("Secondary Owners", r.SecondaryOwners)

	if r.CustomJustification != nil {
		fmt.Fprintln(w)
		field("Custom Justification", *r.CustomJustification)
	}

	if r.TermsAndConditions != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Terms and Conditions:\n")
		for _, line := range strings.Split(*r.TermsAndConditions, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	if r.RequestForm != nil {
		f := r.RequestForm
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Request Form:\n")
		fmt.Fprintf(w, "  %-18s%s\n", "Account:", f.Account)
		if len(f.AccountOptions) > 1 {
			fmt.Fprintf(w, "  %-18s%s\n", "Account Options:", strings.Join(f.AccountOptions, ", "))
		}
		fmt.Fprintf(w, "  Permissions:\n")
		for _, p := range f.Permissions {
			marker := "  "
			if p.Selected {
				marker = "* "
			}
			fmt.Fprintf(w, "    %s%s\n", marker, p.Name)
		}
		fmt.Fprintf(w, "  %-18s%v\n", "Terms Checkbox:", f.HasTermsCheckbox)
		fmt.Fprintf(w, "  %-18s%v\n", "Justification:", f.HasJustification)
	}

	if m != nil {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Current Membership:\n")
		fmt.Fprintf(w, "  %-18s%s\n", "Role:", m.Role)
		fmt.Fprintf(w, "  %-18s%s\n", "Expires:", m.ExpirationDate)
		status := "current"
		if m.Expiring {
			status = "expiring"
		}
		fmt.Fprintf(w, "  %-18s%s\n", "Status:", status)
	}

	if r.Error != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Error: %s\n", r.Error)
	}
}
