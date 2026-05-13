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

	var memberships []Assignment
	if data, err := os.ReadFile(membershipsPath); err == nil {
		_ = yaml.Unmarshal(data, &memberships)
	}
	membershipByID := make(map[string]*Assignment, len(memberships))
	for i := range memberships {
		membershipByID[memberships[i].ID] = &memberships[i]
	}

	if len(args) > 0 {
		details = filterDetails(details, args)
	}

	if len(details) == 0 {
		logHuman("No matching resources found.\n")
		return nil
	}

	w := os.Stdout
	for i, res := range details {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
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

func printResource(w io.Writer, r Resource, m *Assignment) {
	field := func(label, value string) {
		if value != "" {
			_, _ = fmt.Fprintf(w, "%-20s%s\n", label+":", value)
		}
	}
	fieldList := func(label string, values []string) {
		if len(values) == 0 {
			_, _ = fmt.Fprintf(w, "%-20s<none>\n", label+":")
			return
		}
		for i, v := range values {
			if i == 0 {
				_, _ = fmt.Fprintf(w, "%-20s%s\n", label+":", v)
			} else {
				_, _ = fmt.Fprintf(w, "%-20s%s\n", "", v)
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
		_, _ = fmt.Fprintf(w, "%-20s%s\n", "Description:", r.Description)
	}

	_, _ = fmt.Fprintln(w)
	fieldList("Primary Owners", r.PrimaryOwners)
	fieldList("Secondary Owners", r.SecondaryOwners)

	if r.CustomJustification != nil {
		_, _ = fmt.Fprintln(w)
		field("Custom Justification", *r.CustomJustification)
	}

	if r.TermsAndConditions != nil {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Terms and Conditions:\n")
		for _, line := range strings.Split(*r.TermsAndConditions, "\n") {
			_, _ = fmt.Fprintf(w, "  %s\n", line)
		}
	}

	if r.RequestForm != nil {
		f := r.RequestForm
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Request Form:\n")
		_, _ = fmt.Fprintf(w, "  %-18s%s\n", "Account:", f.Account)
		if len(f.AccountOptions) > 1 {
			_, _ = fmt.Fprintf(w, "  %-18s%s\n", "Account Options:", strings.Join(f.AccountOptions, ", "))
		}
		_, _ = fmt.Fprintf(w, "  Permissions:\n")
		for _, p := range f.Permissions {
			marker := "  "
			if p.Selected {
				marker = "* "
			}
			_, _ = fmt.Fprintf(w, "    %s%s\n", marker, p.Name)
		}
		_, _ = fmt.Fprintf(w, "  %-18s%v\n", "Terms Checkbox:", f.HasTermsCheckbox)
		_, _ = fmt.Fprintf(w, "  %-18s%v\n", "Justification:", f.HasJustification)
	}

	if m != nil {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Current Membership:\n")
		_, _ = fmt.Fprintf(w, "  %-18s%s\n", "Role:", m.Role)
		_, _ = fmt.Fprintf(w, "  %-18s%s\n", "Expires:", m.ExpirationDate)
		status := "current"
		if m.Expiring {
			status = "expiring"
		}
		_, _ = fmt.Fprintf(w, "  %-18s%s\n", "Status:", status)
	}

	if r.Error != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Error: %s\n", r.Error)
	}
}
