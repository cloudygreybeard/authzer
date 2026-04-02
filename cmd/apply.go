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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// actionItem pairs a policy rule or portal membership with its
// reconciliation action. For undeclared memberships, rule is zero-valued.
type actionItem struct {
	rule       Rule
	membership *Membership
	action     string
}

var applyCmd = &cobra.Command{
	Use:   "apply [RESOURCE...]",
	Short: "Reconcile memberships against RBAC policy",
	Long: `Compare current portal memberships against the declared RBAC policy
for the configured group and take corrective action:

  - Memberships that exist and are expiring: renew via the memberships page.
  - Memberships required by policy but absent: request via individual pages.
  - Memberships that are current and valid: skip (no action needed).

Portal memberships not covered by any policy rule ("undeclared") are
still included in reconciliation: expiring undeclared memberships are
renewed automatically using the group's default justification.

If RESOURCE arguments are given, only matching resources from the RBAC
policy are processed. Without arguments, all resources from the group
binding are reconciled, plus any undeclared portal memberships.

The dry-run mode controls execution depth:
  --dry-run=client  Resolve policy, show plan; no browser contact.
  --dry-run=server  Open tabs, fill forms, but stop before submit (default).
                    Tabs are left open for manual review and submission.
  --dry-run=none    Full execution: fill forms, submit, and close tabs.`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().Bool("accept-terms", false,
		"automatically accept terms and conditions checkboxes")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	mode := dryRunMode()
	endpoint := cdpURL()
	settleDelay := viper.GetDuration("settleDelay")
	timeout := viper.GetDuration("survey.timeout")
	verbose := viper.GetBool("verbose")
	concurrency := viper.GetInt("concurrency")
	renewWithinDays := viper.GetInt("renewWithinDays")
	acceptTerms, _ := cmd.Flags().GetBool("accept-terms")

	group, err := requireGroup()
	if err != nil {
		return err
	}

	policy, err := loadPolicy()
	if err != nil {
		return fmt.Errorf("loading policy: %w", err)
	}

	rules, justification, err := policy.Resolve(group)
	if err != nil {
		return fmt.Errorf("resolving RBAC policy: %w", err)
	}

	if len(args) > 0 {
		rules, err = filterRules(rules, args)
		if err != nil {
			return err
		}
	}

	if override := viper.GetString("justification"); override != "" {
		justification = override
	}

	fmt.Fprintf(os.Stderr, "Group:         %s\n", group)
	fmt.Fprintf(os.Stderr, "Justification: %s\n", justification)
	fmt.Fprintf(os.Stderr, "Rules:         %d (from RBAC policy)\n", len(rules))
	fmt.Fprintf(os.Stderr, "Renew within:  %d days\n", renewWithinDays)
	fmt.Fprintf(os.Stderr, "Dry-run:       %s\n\n", mode)

	if mode == DryRunClient {
		return runApplyClient(rules, justification, renewWithinDays)
	}

	if err := checkCDP(endpoint); err != nil {
		return err
	}

	wsBase := strings.Replace(endpoint, "http://", "ws://", 1)
	fmt.Fprintf(os.Stderr, "Connecting to browser at %s…\n\n", wsBase)

	browserCtx, browserCancel := connectBrowser(ctx, wsBase, verbose)
	defer browserCancel()

	opts := surveyOpts{
		SettleDelay: settleDelay,
		Timeout:     timeout,
		Verbose:     verbose,
	}

	fmt.Fprintf(os.Stderr, "Fetching current memberships…\n")
	memberships, err := listMemberships(browserCtx, opts)
	if err != nil {
		return fmt.Errorf("listing memberships: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d current memberships.\n\n", len(memberships))

	cacheDir := cacheDirectory()
	membershipsCachePath := filepath.Join(cacheDir, "memberships-cache.yaml")
	_ = writeMembershipsCache(membershipsCachePath, memberships)

	membershipByName := make(map[string]*Membership, len(memberships))
	for i := range memberships {
		membershipByName[memberships[i].Name] = &memberships[i]
	}

	detailsCache, _ := readCache(filepath.Join(cacheDir, "details-cache.yaml"))
	nameByID := buildNameLookup(detailsCache)
	termsLookup := buildTermsLookup(detailsCache)
	termsTextLookup := buildTermsTextLookup(detailsCache)

	var actionable []actionItem
	var current, skipped int

	threshold := time.Now().AddDate(0, 0, renewWithinDays)
	claimedNames := make(map[string]bool, len(rules))
	n := 0

	termsNote := func(name string) string {
		if termsLookup[name] {
			return " [has T&Cs]"
		}
		return ""
	}

	for _, rule := range rules {
		n++
		displayName := nameByID[rule.Resource]
		m := membershipByName[displayName]

		if m != nil {
			claimedNames[m.Name] = true
			expiresWithin := isExpiringWithin(m.ExpirationDate, threshold)
			if expiresWithin || m.Expiring {
				fmt.Fprintf(os.Stderr, "  [%d] %s — EXTEND (expires %s)%s\n",
					n, m.Name, m.ExpirationDate, termsNote(m.Name))
				actionable = append(actionable, actionItem{
					rule: rule, membership: m, action: "renew",
				})
			} else {
				fmt.Fprintf(os.Stderr, "  [%d] %s — current (expires %s)\n",
					n, m.Name, m.ExpirationDate)
				current++
			}
		} else {
			name := rule.Resource
			if name == "" {
				name = rule.SelfLink
			}
			if rule.SelfLink == "" {
				fmt.Fprintf(os.Stderr, "  [%d] %s — SKIP (no selfLink for request)\n",
					n, name)
				skipped++
				continue
			}
			fmt.Fprintf(os.Stderr, "  [%d] %s — REQUEST (not in current memberships)%s\n",
				n, name, termsNote(name))
			actionable = append(actionable, actionItem{
				rule: rule, action: "request",
			})
		}
	}

	var undeclared int
	if len(args) == 0 {
		for i := range memberships {
			m := &memberships[i]
			if claimedNames[m.Name] {
				continue
			}
			n++
			expiresWithin := isExpiringWithin(m.ExpirationDate, threshold)
			if expiresWithin || m.Expiring {
				fmt.Fprintf(os.Stderr, "  [%d] %s — EXTEND undeclared (expires %s)%s\n",
					n, m.Name, m.ExpirationDate, termsNote(m.Name))
				actionable = append(actionable, actionItem{
					membership: m, action: "renew",
				})
			} else {
				fmt.Fprintf(os.Stderr, "  [%d] %s — current, undeclared (expires %s)\n",
					n, m.Name, m.ExpirationDate)
				current++
			}
			undeclared++
		}
	}

	fmt.Fprintf(os.Stderr, "\n────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "Policy: %d  Portal: %d (undeclared: %d)\n",
		len(rules), len(memberships), undeclared)
	fmt.Fprintf(os.Stderr, "Renew: %d  Request: %d  Current: %d  Skipped: %d\n",
		countAction(actionable, "renew"), countAction(actionable, "request"),
		current, skipped)

	if !acceptTerms {
		hasAnyTerms := false
		for _, it := range actionable {
			name := it.rule.Resource
			if it.membership != nil {
				name = it.membership.Name
			}
			if termsLookup[name] {
				hasAnyTerms = true
				break
			}
		}
		if hasAnyTerms {
			fmt.Fprintf(os.Stderr, "\nNote: some resources have T&Cs. Pass --accept-terms to tick checkboxes automatically.\n")
		}
	}

	if len(actionable) == 0 {
		fmt.Fprintf(os.Stderr, "\nAll memberships are current. Nothing to do.\n")
		return nil
	}

	var renewals, requests []actionItem
	for _, it := range actionable {
		switch it.action {
		case "renew":
			renewals = append(renewals, it)
		default:
			requests = append(requests, it)
		}
	}

	var completed atomic.Int32
	var succeeded, failed int
	totalActions := len(actionable)

	reportResult := func(it actionItem, res Resource) {
		n := completed.Add(1)
		label := it.rule.Resource
		if it.membership != nil {
			label = it.membership.Name
		}
		if termsText := termsTextLookup[label]; termsText != "" && termsLookup[label] {
			if acceptTerms {
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s T&Cs: %s\n",
					n, totalActions, it.action, label, termsText)
			}
		}
		if res.Error != "" {
			if mode == DryRunServer {
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s … FAILED, tab open (%s)\n",
					n, totalActions, it.action, label, res.Error)
			} else {
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s … FAILED (%s)\n",
					n, totalActions, it.action, label, res.Error)
			}
			failed++
		} else {
			switch mode {
			case DryRunServer:
				hasTerms := termsLookup[label]
				if hasTerms && !acceptTerms {
					fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s … prepared, terms not accepted (tab open)\n",
						n, totalActions, it.action, label)
				} else {
					fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s … prepared (tab open)\n",
						n, totalActions, it.action, label)
				}
			case DryRunNone:
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s/%s … done\n",
					n, totalActions, it.action, label)
			}
			succeeded++
		}
	}

	for _, it := range renewals {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\nAborted.\n")
			goto done
		default:
		}

		rOpts := renewOpts{
			SettleDelay:   settleDelay,
			Timeout:       timeout,
			Verbose:       verbose,
			Justification: justification,
			DryRun:        mode,
			AcceptTerms:   acceptTerms,
		}
		res := renewMembership(browserCtx, it.membership.Name, rOpts)
		reportResult(it, res)
	}

	{
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup

	requestLoop:
		for _, item := range requests {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				fmt.Fprintf(os.Stderr, "\nAborted.\n")
				break requestLoop
			}

			wg.Add(1)
			go func(it actionItem) {
				defer wg.Done()
				defer func() { <-sem }()

				rOpts := renewOpts{
					SettleDelay:   settleDelay,
					Timeout:       timeout,
					Verbose:       verbose,
					Permission:    it.rule.Permission,
					Justification: justification,
					DryRun:        mode,
					AcceptTerms:   acceptTerms,
				}
				kind := it.rule.Kind
				if kind == "" {
					kind = "Resource"
				}
				res := renewResource(browserCtx, it.rule.SelfLink, kind, rOpts)
				reportResult(it, res)
			}(item)
		}
		wg.Wait()
	}

done:

	fmt.Fprintf(os.Stderr, "\n────────────────────────────────\n")
	switch mode {
	case DryRunServer:
		fmt.Fprintf(os.Stderr, "Done. %d forms prepared, %d failed. Tabs left open for review.\n",
			succeeded, failed)
	case DryRunNone:
		fmt.Fprintf(os.Stderr, "Done. %d applied, %d failed.\n",
			succeeded, failed)
	}

	return nil
}

func runApplyClient(rules []Rule, justification string, renewWithinDays int) error {
	cacheDir := cacheDirectory()
	membershipsCachePath := filepath.Join(cacheDir, "memberships-cache.yaml")

	var memberships []Membership
	if data, err := os.ReadFile(membershipsCachePath); err == nil {
		_ = yaml.Unmarshal(data, &memberships)
	}

	membershipByName := make(map[string]*Membership, len(memberships))
	for i := range memberships {
		membershipByName[memberships[i].Name] = &memberships[i]
	}

	detailsCache, _ := readCache(filepath.Join(cacheDir, "details-cache.yaml"))
	nameByID := buildNameLookup(detailsCache)
	termsLookup := buildTermsLookup(detailsCache)

	threshold := time.Now().AddDate(0, 0, renewWithinDays)
	var extendCount, requestCount, currentCount, undeclared int
	claimedNames := make(map[string]bool, len(rules))
	n := 0

	termsNote := func(name string) string {
		if termsLookup[name] {
			return " [has T&Cs]"
		}
		return ""
	}

	for _, rule := range rules {
		n++
		displayName := nameByID[rule.Resource]
		m := membershipByName[displayName]
		if m != nil {
			claimedNames[m.Name] = true
			expiresWithin := isExpiringWithin(m.ExpirationDate, threshold)
			if expiresWithin || m.Expiring {
				fmt.Fprintf(os.Stderr, "  [%d] %s — would EXTEND (expires %s)%s\n",
					n, m.Name, m.ExpirationDate, termsNote(m.Name))
				extendCount++
			} else {
				fmt.Fprintf(os.Stderr, "  [%d] %s — current (expires %s)\n",
					n, m.Name, m.ExpirationDate)
				currentCount++
			}
		} else {
			name := rule.Resource
			if name == "" {
				name = rule.SelfLink
			}
			fmt.Fprintf(os.Stderr, "  [%d] %s — would REQUEST (not in memberships)%s\n",
				n, name, termsNote(name))
			requestCount++
		}
	}

	for i := range memberships {
		m := &memberships[i]
		if claimedNames[m.Name] {
			continue
		}
		n++
		expiresWithin := isExpiringWithin(m.ExpirationDate, threshold)
		if expiresWithin || m.Expiring {
			fmt.Fprintf(os.Stderr, "  [%d] %s — would EXTEND undeclared (expires %s)%s\n",
				n, m.Name, m.ExpirationDate, termsNote(m.Name))
			extendCount++
		} else {
			fmt.Fprintf(os.Stderr, "  [%d] %s — current, undeclared (expires %s)\n",
				n, m.Name, m.ExpirationDate)
			currentCount++
		}
		undeclared++
	}

	fmt.Fprintf(os.Stderr, "\n────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "Policy: %d  Portal: %d (undeclared: %d)\n",
		len(rules), len(memberships), undeclared)
	fmt.Fprintf(os.Stderr, "Renew: %d  Request: %d  Current: %d\n",
		extendCount, requestCount, currentCount)

	if len(memberships) == 0 {
		fmt.Fprintf(os.Stderr, "\nNote: no cached membership data. Run 'authzer get' first for accurate reconciliation.\n")
	}

	fmt.Fprintf(os.Stderr, "\nClient dry-run complete.\n")
	return nil
}

// buildNameLookup creates a map from resource ID (slug) to display name
// using cached deep survey data.
func buildNameLookup(details []Resource) map[string]string {
	m := make(map[string]string, len(details))
	for _, d := range details {
		if d.ID != "" && d.Name != "" {
			m[d.ID] = d.Name
		}
	}
	return m
}

// buildTermsLookup creates a map from resource name to whether the
// resource has a T&Cs checkbox, using cached deep survey data.
func buildTermsLookup(details []Resource) map[string]bool {
	m := make(map[string]bool, len(details))
	for _, d := range details {
		if d.Name != "" && d.RequestForm != nil && d.RequestForm.HasTermsCheckbox {
			m[d.Name] = true
		}
	}
	return m
}

// buildTermsTextLookup creates a map from resource name to the T&Cs
// text content, using cached deep survey data.
func buildTermsTextLookup(details []Resource) map[string]string {
	m := make(map[string]string, len(details))
	for _, d := range details {
		if d.Name == "" {
			continue
		}
		if d.RequestForm != nil && d.RequestForm.TermsText != "" {
			m[d.Name] = d.RequestForm.TermsText
		} else if d.TermsAndConditions != nil && *d.TermsAndConditions != "" {
			m[d.Name] = *d.TermsAndConditions
		}
	}
	return m
}

func isExpiringWithin(dateStr string, threshold time.Time) bool {
	formats := []string{
		"January 02, 2006 03:04 PM MST",
		"January 2, 2006 03:04 PM MST",
		"Jan 02, 2006 03:04 PM MST",
		"Jan 2, 2006 03:04 PM MST",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t.Before(threshold)
		}
	}
	return false
}

func countAction(items []actionItem, action string) int {
	count := 0
	for _, it := range items {
		if it.action == action {
			count++
		}
	}
	return count
}
