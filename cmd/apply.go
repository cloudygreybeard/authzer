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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

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
	applyCmd.Flags().String("renew-within", "",
		`renewal threshold as a number of days, e.g. "30d" or "30" (default from config)`)
	applyCmd.Flags().StringP("output", "o", "", `output format: "yaml", "json"`)
	applyCmd.Flags().String("sort-by", "",
		`sort order for displayed memberships: "expiry", "name" (default: policy order)`)
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	mode := dryRunMode()
	endpoint := cdpURL()
	settleDelay := viper.GetDuration("settleDelay")
	timeout := viper.GetDuration("survey.timeout")
	concurrency := viper.GetInt("concurrency")
	acceptTerms, _ := cmd.Flags().GetBool("accept-terms")
	outputFormat, _ := cmd.Flags().GetString("output")
	sortBy, _ := cmd.Flags().GetString("sort-by")

	renewWithinDays := viper.GetInt("renewWithinDays")
	if rw, _ := cmd.Flags().GetString("renew-within"); rw != "" {
		d, err := parseDays(rw)
		if err != nil {
			return fmt.Errorf("--renew-within: %w", err)
		}
		renewWithinDays = d
	}

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

	logHuman("Group:         %s\n", group)
	logHuman("Justification: %s\n", justification)
	logHuman("Rules:         %d (from RBAC policy)\n", len(rules))
	logHuman("Renew within:  %d days\n", renewWithinDays)
	logHuman("Dry-run:       %s\n\n", mode)

	auditLog.Info("apply.start", map[string]any{
		"group":         group,
		"justification": justification,
		"rules":         len(rules),
		"renewWithin":   renewWithinDays,
		"dryRun":        mode,
		"args":          args,
	})

	if mode == DryRunClient {
		return runApplyClient(rules, justification, renewWithinDays, group, outputFormat, sortBy, args)
	}

	if err := checkCDP(endpoint); err != nil {
		return err
	}

	wsBase := strings.Replace(endpoint, "http://", "ws://", 1)
	logHuman("Connecting to browser at %s…\n\n", wsBase)

	browserCtx, browserCancel := connectBrowser(ctx, wsBase)
	if mode != DryRunServer {
		defer browserCancel()
	}

	opts := surveyOpts{
		SettleDelay: settleDelay,
		Timeout:     timeout,
	}

	reg, err := initBrowserRegistry(browserCtx, opts)
	if err != nil {
		return err
	}
	provider, err := reg.ForKind("Entitlement")
	if err != nil {
		return fmt.Errorf("no provider for Entitlement kind: %w", err)
	}

	logHuman("Fetching current memberships via %s…\n", provider.Name())
	memberships, err := provider.List(ctx)
	if err != nil {
		return fmt.Errorf("listing memberships: %w", err)
	}
	logHuman("Found %d current memberships.\n\n", len(memberships))

	cacheDir := cacheDirectory()
	membershipsCachePath := filepath.Join(cacheDir, "memberships-cache.yaml")
	_ = writeMembershipsCache(membershipsCachePath, memberships)

	detailsCache, _ := readCache(filepath.Join(cacheDir, "details-cache.yaml"))
	nameByID := buildNameLookup(detailsCache)
	termsLookup := buildTermsLookup(detailsCache)
	termsTextLookup := buildTermsTextLookup(detailsCache)

	excludeList := viper.GetStringSlice("excludeResources")
	excluded := make(map[string]bool, len(excludeList))
	for _, e := range excludeList {
		excluded[strings.ToLower(strings.TrimSpace(e))] = true
	}

	reconcileInput := ReconcileInput{
		Rules:       rules,
		Assignments: memberships,
		NameByID:    nameByID,
		RenewWithin: time.Duration(renewWithinDays) * 24 * time.Hour,
		ExcludeList: excluded,
		Targeted:    args,
	}
	allActions := Reconcile(reconcileInput)
	actionable := ActionableItems(allActions)
	counts := CountByAction(allActions)

	termsNote := func(name string) string {
		if termsLookup[name] {
			return "[has T&Cs]"
		}
		return ""
	}

	var renewItems, requestItems, currentItems, excludedItems []summaryItem
	for _, ra := range allActions {
		name := ra.Rule.Resource
		if ra.Assignment != nil {
			name = ra.Assignment.Name
		}
		expires := ""
		if ra.Assignment != nil {
			expires = ra.Assignment.ExpirationDate
		}
		si := summaryItem{name: name, expires: expires}
		if strings.Contains(ra.Reason, "undeclared") {
			si.action = "undeclared"
		}

		switch ra.Action {
		case "renew":
			si.note = termsNote(name)
			renewItems = append(renewItems, si)
		case "request":
			si.note = termsNote(name)
			requestItems = append(requestItems, si)
		case "current":
			currentItems = append(currentItems, si)
		case "excluded":
			excludedItems = append(excludedItems, si)
		}
	}

	maxName := 0
	allSummary := make([]summaryItem, 0, len(renewItems)+len(requestItems)+len(currentItems)+len(excludedItems))
	allSummary = append(allSummary, renewItems...)
	allSummary = append(allSummary, requestItems...)
	allSummary = append(allSummary, currentItems...)
	allSummary = append(allSummary, excludedItems...)
	for _, it := range allSummary {
		if len(it.name) > maxName {
			maxName = len(it.name)
		}
	}

	printGroup := func(label string, items []summaryItem) {
		if len(items) == 0 {
			return
		}
		logHuman("%s:\n", label)
		for _, it := range items {
			annotations := ""
			if it.action == "undeclared" {
				annotations += " [undeclared]"
			}
			if it.note != "" {
				annotations += " " + it.note
			}
			if it.expires != "" {
				logHuman("  %-*s  (expires %s)%s\n",
					maxName, it.name, it.expires, annotations)
			} else {
				logHuman("  %-*s  (not in current memberships)%s\n",
					maxName, it.name, annotations)
			}
		}
	}

	if sortBy != "" {
		sortSummaryItems(renewItems, sortBy)
		sortSummaryItems(requestItems, sortBy)
		sortSummaryItems(currentItems, sortBy)
	}

	printGroup("renew", renewItems)
	printGroup("request", requestItems)
	printGroup("current", currentItems)
	printGroup("excluded", excludedItems)

	logHuman("\n")
	logHuman("Policy: %d  Portal: %d (excluded: %d)\n",
		len(rules), len(memberships), counts["excluded"])
	logHuman("Renew: %d  Request: %d  Current: %d\n",
		counts["renew"], counts["request"], counts["current"])

	auditLog.Info("apply.plan", map[string]any{
		"renew":    counts["renew"],
		"request":  counts["request"],
		"current":  counts["current"],
		"excluded": counts["excluded"],
		"policy":   len(rules),
		"portal":   len(memberships),
	})

	if !acceptTerms {
		hasAnyTerms := false
		for _, ra := range actionable {
			name := ra.Rule.Resource
			if ra.Assignment != nil {
				name = ra.Assignment.Name
			}
			if termsLookup[name] {
				hasAnyTerms = true
				break
			}
		}
		if hasAnyTerms {
			logHuman("\nNote: some resources have T&Cs. Pass --accept-terms to tick checkboxes automatically.\n")
		}
	}

	if len(actionable) == 0 {
		logHuman("\nAll memberships are current. Nothing to do.\n")
		auditLog.Info("apply.done", map[string]any{"result": "nothing_to_do"})
		return nil
	}

	isDryRun := mode != DryRunNone
	var completed atomic.Int32
	var succeeded, failed int
	totalActions := len(actionable)

	var resultsMu sync.Mutex
	var results []Action

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

applyLoop:
	for _, item := range actionable {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			logHuman("\nAborted.\n")
			auditLog.Warn("apply.aborted", map[string]any{"phase": item.Action})
			break applyLoop
		}

		wg.Add(1)
		go func(ra ReconcileAction) {
			defer wg.Done()
			defer func() { <-sem }()

			label := ra.Rule.Resource
			if ra.Assignment != nil {
				label = ra.Assignment.Name
			}

			if termsText := termsTextLookup[label]; termsText != "" && termsLookup[label] && acceptTerms {
				logHuman("  [%d/%d] %s/%s T&Cs: %s\n",
					completed.Load()+1, totalActions, ra.Action, label, termsText)
				auditLog.Info("apply.terms", map[string]any{
					"name":  label,
					"terms": termsText,
				})
			}

			result, applyErr := provider.Apply(ctx, ra.Rule, justification, isDryRun)
			n := completed.Add(1)

			act := Action{
				Name:   label,
				Action: ra.Action,
			}
			if ra.Assignment != nil {
				act.ID = ra.Assignment.ID
				act.CurrentRole = ra.Assignment.Role
			}
			if ra.Rule.Resource != "" {
				act.ID = ra.Rule.Resource
				act.DesiredRole = ra.Rule.Permission
				act.SelfLink = ra.Rule.SelfLink
			}

			if applyErr != nil {
				act.Error = applyErr.Error()
				act.Reason = "failed"
				logHuman("  [%d/%d] %s/%s … FAILED (%v)\n", n, totalActions, ra.Action, label, applyErr)
				auditLog.Error("apply."+ra.Action+".fail", act)
				resultsMu.Lock()
				failed++
				results = append(results, act)
				resultsMu.Unlock()
				return
			}

			act.Reason = result.Action
			if result.Error != nil {
				act.Error = result.Error.Error()
				act.Reason = "failed"
				logHuman("  [%d/%d] %s/%s … FAILED (%s)\n", n, totalActions, ra.Action, label, result.Message)
				auditLog.Error("apply."+ra.Action+".fail", act)
				resultsMu.Lock()
				failed++
				results = append(results, act)
				resultsMu.Unlock()
				return
			}

			logHuman("  [%d/%d] %s/%s … %s\n", n, totalActions, ra.Action, label, result.Message)
			auditLog.Info("apply."+ra.Action+".ok", act)
			resultsMu.Lock()
			succeeded++
			results = append(results, act)
			resultsMu.Unlock()
		}(item)
	}
	wg.Wait()

	logHuman("\n")
	if isDryRun {
		logHuman("Done. %d prepared, %d failed.\n", succeeded, failed)
	} else {
		logHuman("Done. %d applied, %d failed.\n", succeeded, failed)
	}

	auditLog.Info("apply.done", map[string]any{
		"succeeded": succeeded,
		"failed":    failed,
		"dryRun":    mode,
	})

	if outputFormat != "" {
		data := ApplyData{
			Updated:       time.Now().UTC().Format(time.RFC3339),
			Group:         group,
			Justification: justification,
			DryRun:        mode,
			TotalItems:    len(results),
			Summary: ApplySummary{
				Renew:   counts["renew"],
				Request: counts["request"],
				Current: counts["current"],
				Failed:  failed,
			},
			Items: results,
		}
		return printApplyOutput(data, outputFormat)
	}

	return nil
}

func printApplyOutput(data ApplyData, format string) error {
	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "ApplyResult",
		Data:       data,
	}

	var out []byte
	var err error

	switch format {
	case "yaml":
		out, err = yaml.Marshal(&envelope)
	case "json":
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		err = enc.Encode(&envelope)
		out = buf.Bytes()
	default:
		return fmt.Errorf("unsupported output format: %q (use yaml or json)", format)
	}
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}

	_, err = os.Stdout.Write(out)
	return err
}

func runApplyClient(rules []Rule, justification string, renewWithinDays int, group, outputFormat, sortBy string, args []string) error {
	cacheDir := cacheDirectory()
	membershipsCachePath := filepath.Join(cacheDir, "memberships-cache.yaml")

	var memberships []Assignment
	if data, err := os.ReadFile(membershipsCachePath); err == nil {
		_ = yaml.Unmarshal(data, &memberships)
	}

	membershipByName := make(map[string]*Assignment, len(memberships))
	for i := range memberships {
		membershipByName[memberships[i].Name] = &memberships[i]
	}

	detailsCache, _ := readCache(filepath.Join(cacheDir, "details-cache.yaml"))
	nameByID := buildNameLookup(detailsCache)
	termsLookup := buildTermsLookup(detailsCache)

	threshold := time.Now().AddDate(0, 0, renewWithinDays)
	claimedNames := make(map[string]bool, len(rules))

	excludeList := viper.GetStringSlice("excludeResources")
	excluded := make(map[string]bool, len(excludeList))
	for _, e := range excludeList {
		excluded[strings.ToLower(strings.TrimSpace(e))] = true
	}
	isExcluded := func(name string) bool {
		return excluded[strings.ToLower(name)]
	}

	var renewItems, requestItems, currentItems, excludedItems []summaryItem
	var items []Action

	termsNote := func(name string) string {
		if termsLookup[name] {
			return "[has T&Cs]"
		}
		return ""
	}

	for _, rule := range rules {
		displayName := nameByID[rule.Resource]
		m := membershipByName[displayName]
		if m != nil {
			claimedNames[m.Name] = true
			if isExcluded(m.Name) {
				excludedItems = append(excludedItems, summaryItem{
					name: m.Name, expires: m.ExpirationDate,
				})
				items = append(items, Action{
					ID: rule.Resource, Name: m.Name, Action: "none",
					Reason: "excluded", CurrentRole: m.Role, DesiredRole: rule.Permission,
				})
				continue
			}
			targeted := len(args) > 0
			if targeted || isExpiringWithin(m.ExpirationDate, threshold) {
				renewItems = append(renewItems, summaryItem{
					name: m.Name, expires: m.ExpirationDate, note: termsNote(m.Name),
				})
				items = append(items, Action{
					ID: rule.Resource, Name: m.Name, Action: "renew",
					Reason: "expiring", CurrentRole: m.Role, DesiredRole: rule.Permission,
					SelfLink: rule.SelfLink,
				})
			} else {
				currentItems = append(currentItems, summaryItem{
					name: m.Name, expires: m.ExpirationDate,
				})
				items = append(items, Action{
					ID: rule.Resource, Name: m.Name, Action: "none",
					Reason: "current", CurrentRole: m.Role, DesiredRole: rule.Permission,
				})
			}
		} else {
			name := rule.Resource
			if name == "" {
				name = rule.SelfLink
			}
			requestItems = append(requestItems, summaryItem{
				name: name, note: termsNote(name),
			})
			items = append(items, Action{
				ID: rule.Resource, Name: name, Action: "request",
				Reason: "missing", DesiredRole: rule.Permission, SelfLink: rule.SelfLink,
			})
		}
	}

	var undeclared int
	if len(args) > 0 {
		goto skipUndeclared
	}
	for i := range memberships {
		m := &memberships[i]
		if claimedNames[m.Name] {
			continue
		}
		if isExcluded(m.Name) {
			excludedItems = append(excludedItems, summaryItem{
				name: m.Name, expires: m.ExpirationDate, action: "undeclared",
			})
			items = append(items, Action{
				ID: m.ID, Name: m.Name, Action: "none",
				Reason: "excluded, undeclared", CurrentRole: m.Role,
			})
			undeclared++
			continue
		}
		if isExpiringWithin(m.ExpirationDate, threshold) {
			renewItems = append(renewItems, summaryItem{
				name: m.Name, expires: m.ExpirationDate,
				note: termsNote(m.Name), action: "undeclared",
			})
			items = append(items, Action{
				ID: m.ID, Name: m.Name, Action: "renew",
				Reason: "expiring, undeclared", CurrentRole: m.Role,
			})
		} else {
			currentItems = append(currentItems, summaryItem{
				name: m.Name, expires: m.ExpirationDate, action: "undeclared",
			})
			items = append(items, Action{
				ID: m.ID, Name: m.Name, Action: "none",
				Reason: "current, undeclared", CurrentRole: m.Role,
			})
		}
		undeclared++
	}
skipUndeclared:

	maxName := 0
	all := make([]summaryItem, 0, len(renewItems)+len(requestItems)+len(currentItems))
	all = append(all, renewItems...)
	all = append(all, requestItems...)
	all = append(all, currentItems...)
	all = append(all, excludedItems...)
	for _, it := range all {
		if len(it.name) > maxName {
			maxName = len(it.name)
		}
	}

	printGroup := func(label string, sitems []summaryItem) {
		if len(sitems) == 0 {
			return
		}
		logHuman("%s:\n", label)
		for _, it := range sitems {
			annotations := ""
			if it.action == "undeclared" {
				annotations += " [undeclared]"
			}
			if it.note != "" {
				annotations += " " + it.note
			}
			if it.expires != "" {
				logHuman("  %-*s  (expires %s)%s\n",
					maxName, it.name, it.expires, annotations)
			} else {
				logHuman("  %-*s  (not in current memberships)%s\n",
					maxName, it.name, annotations)
			}
		}
	}

	if sortBy != "" {
		sortSummaryItems(renewItems, sortBy)
		sortSummaryItems(requestItems, sortBy)
		sortSummaryItems(currentItems, sortBy)
	}

	printGroup("renew", renewItems)
	printGroup("request", requestItems)
	printGroup("current", currentItems)
	printGroup("excluded", excludedItems)

	logHuman("\n")
	logHuman("Policy: %d  Portal: %d (undeclared: %d, excluded: %d)\n",
		len(rules), len(memberships), undeclared, len(excludedItems))
	logHuman("Renew: %d  Request: %d  Current: %d\n",
		len(renewItems), len(requestItems), len(currentItems))

	auditLog.Info("apply.plan", map[string]any{
		"renew":    len(renewItems),
		"request":  len(requestItems),
		"current":  len(currentItems),
		"excluded": len(excludedItems),
		"policy":   len(rules),
		"portal":   len(memberships),
		"dryRun":   DryRunClient,
	})

	if len(memberships) == 0 {
		logHuman("\nNote: no cached membership data. Run 'authzer get' first for accurate reconciliation.\n")
	}

	logHuman("\nClient dry-run complete.\n")
	auditLog.Info("apply.done", map[string]any{
		"dryRun":  DryRunClient,
		"renew":   len(renewItems),
		"request": len(requestItems),
		"current": len(currentItems),
	})

	if outputFormat != "" {
		data := ApplyData{
			Updated:       time.Now().UTC().Format(time.RFC3339),
			Group:         group,
			Justification: justification,
			DryRun:        DryRunClient,
			TotalItems:    len(items),
			Summary: ApplySummary{
				Renew:   len(renewItems),
				Request: len(requestItems),
				Current: len(currentItems),
			},
			Items: items,
		}
		return printApplyOutput(data, outputFormat)
	}

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

type summaryItem struct {
	name    string
	expires string
	note    string
	action  string
}

func sortSummaryItems(items []summaryItem, sortBy string) {
	switch sortBy {
	case "expiry":
		sort.SliceStable(items, func(i, j int) bool {
			ti := parseExpiryTime(items[i].expires)
			tj := parseExpiryTime(items[j].expires)
			return ti.Before(tj)
		})
	case "name":
		sort.SliceStable(items, func(i, j int) bool {
			return strings.ToLower(items[i].name) < strings.ToLower(items[j].name)
		})
	}
}

func parseExpiryTime(s string) time.Time {
	formats := []string{
		"January 02, 2006 03:04 PM MST",
		"January 2, 2006 03:04 PM MST",
		"Jan 02, 2006 03:04 PM MST",
		"Jan 2, 2006 03:04 PM MST",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseDays accepts "30d" or "30" and returns the integer number of days.
func parseDays(s string) (int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "d")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid day value %q: expected a number like 30 or 30d", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("day value must be non-negative, got %d", n)
	}
	return n, nil
}

