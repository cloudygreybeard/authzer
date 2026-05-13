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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var getCmd = &cobra.Command{
	Use:   "get [RESOURCE...]",
	Short: "List current memberships and their status",
	Long: `Scrape the memberships table from the portal and display current
membership status including names, roles, and expiration dates.

On first run, deep metadata is collected from individual resource pages
and cached locally. Subsequent runs refresh only the lightweight
membership table. Use --refresh to force re-collection of deep metadata.

Output format:
  (default)          Tabular summary: NAME, ROLE, EXPIRES, STATUS
  -o wide            Tabular with additional columns
  -o yaml            Full structured YAML output
  -o json            Full structured JSON output
  -o name            Resource names only

The dry-run mode controls execution depth:
  --dry-run=client   Resolve policy locally; no browser contact.
  --dry-run=server   Connect to browser and scrape memberships (default).
  --dry-run=none     Same as server for get (read-only operation).`,
	RunE: runGet,
}

func init() {
	getCmd.Flags().StringP("output", "o", "", `output format: "wide", "yaml", "json", "name"`)
	getCmd.Flags().Bool("refresh", false, "force deep re-survey of individual resource pages")
	getCmd.Flags().StringP("file", "f", "", "write output to file (default: stdout)")

	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	mode := dryRunMode()
	outputFormat, _ := cmd.Flags().GetString("output")
	refresh, _ := cmd.Flags().GetBool("refresh")
	outputFile, _ := cmd.Flags().GetString("file")
	endpoint := cdpURL()
	settleDelay := viper.GetDuration("settleDelay")
	timeout := viper.GetDuration("survey.timeout")
	concurrency := viper.GetInt("concurrency")
	backend := viper.GetString("backend")

	logHuman("Dry-run:  %s\n", mode)
	auditLog.Info("get.start", map[string]any{"dryRun": mode, "args": args})

	if mode == DryRunClient {
		logHuman("\nClient dry-run: no browser contact.\n")
		return printCachedGet(outputFormat, outputFile, args)
	}

	if err := checkCDP(endpoint); err != nil {
		return err
	}

	wsBase := strings.Replace(endpoint, "http://", "ws://", 1)
	if backend == "api" {
		logHuman("CDP:      %s (cookie extraction only)\n", endpoint)
	} else {
		logHuman("CDP:      %s\n", endpoint)
	}
	logHuman("\nConnecting to browser at %s…\n\n", wsBase)

	browserCtx, browserCancel := connectBrowser(ctx, wsBase)
	defer browserCancel()

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

	logHuman("Fetching memberships via %s…\n", provider.Name())
	memberships, err := provider.List(ctx)
	if err != nil {
		return fmt.Errorf("listing memberships: %w", err)
	}
	logHuman("Found %d memberships.\n", len(memberships))

	cacheDir := cacheDirectory()
	membershipsCachePath := filepath.Join(cacheDir, "memberships-cache.yaml")
	if err := writeMembershipsCache(membershipsCachePath, memberships); err != nil {
		logHuman("Warning: could not write memberships cache: %v\n", err)
	}

	cachePath := filepath.Join(cacheDir, "details-cache.yaml")
	var details []Resource

	needDeep := backend != "api" && (refresh || !cacheFileExists(cachePath))
	if needDeep {
		group, err := requireGroup()
		if err != nil {
			return err
		}
		rules, _, err := resolveRulesForGroup(group)
		if err != nil {
			return fmt.Errorf("resolving policy rules: %w", err)
		}

		if len(args) > 0 {
			rules, err = filterRules(rules, args)
			if err != nil {
				return err
			}
		}

		logHuman("\nCollecting metadata for %d policy resources…\n", len(rules))
		managed := surveyResources(browserCtx, rules, opts, concurrency)
		boolTrue := true
		for i := range managed {
			managed[i].Managed = &boolTrue
		}

		managedIDs := make(map[string]bool, len(managed))
		for _, d := range managed {
			managedIDs[d.ID] = true
		}

		var undeclaredRules []Rule
		for _, m := range memberships {
			if managedIDs[m.ID] || m.SelfLink == "" {
				continue
			}
			undeclaredRules = append(undeclaredRules, Rule{
				Kind:     "Entitlement",
				Resource: m.Name,
				SelfLink: m.SelfLink,
			})
		}

		var undeclared []Resource
		if len(undeclaredRules) > 0 {
			logHuman("\nCollecting metadata for %d undeclared memberships…\n", len(undeclaredRules))
			undeclared = surveyResources(browserCtx, undeclaredRules, opts, concurrency)
			boolFalse := false
			for i := range undeclared {
				undeclared[i].Managed = &boolFalse
			}
		}

		details = append(managed, undeclared...)

		if err := writeCache(cachePath, details); err != nil {
			logHuman("Warning: could not write cache: %v\n", err)
		} else {
			logHuman("Cached %d resource details to %s\n", len(details), cachePath)
		}
	} else {
		details, _ = readCache(cachePath)
		if len(details) > 0 {
			logV(3, "loaded %d cached resource details from %s", len(details), cachePath)
		}
	}

	outputMemberships := memberships
	outputDetails := details
	if len(args) > 0 {
		outputMemberships = filterAssignments(memberships, args)
		outputDetails = filterDetailsByArgs(details, args)
	} else {
		printComplianceSummary(memberships, details)
	}

	data := GetData{
		Updated:     time.Now().UTC().Format(time.RFC3339),
		CDPEndpoint: endpoint,
		TotalItems:  len(outputMemberships),
		Items:       outputMemberships,
		Details:     outputDetails,
	}

	return printGetOutput(data, outputFormat, outputFile)
}

func resolveRulesForGroup(group string) ([]Rule, string, error) {
	policy, err := loadPolicy()
	if err != nil {
		return nil, "", err
	}
	rules, justification, err := policy.Resolve(group)
	if err != nil {
		return nil, "", err
	}
	return rules, justification, nil
}

func surveyResources(ctx context.Context, rules []Rule, opts surveyOpts, concurrency int) []Resource {
	sem := make(chan struct{}, concurrency)
	results := make([]Resource, len(rules))
	var completed atomic.Int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	total := len(rules)

surveyLoop:
	for i, rule := range rules {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break surveyLoop
		}

		wg.Add(1)
		go func(idx int, r Rule) {
			defer wg.Done()
			defer func() { <-sem }()

			kind := r.Kind
			if kind == "" {
				kind = "Resource"
			}
			res := surveyResource(ctx, r.SelfLink, kind, opts)
			n := completed.Add(1)

			mu.Lock()
			results[idx] = res
			if res.Error != "" {
				logHuman("  [%d/%d] %s … FAILED (%s)\n", n, total, r.SelfLink, res.Error)
			} else {
				logHuman("  [%d/%d] %s … ok\n", n, total, res.Name)
			}
			mu.Unlock()
		}(i, rule)
	}

	wg.Wait()

	var processed []Resource
	for _, r := range results {
		if r.SelfLink != "" {
			processed = append(processed, r)
		}
	}
	return processed
}

func printGetOutput(data GetData, format, outputFile string) error {
	var out []byte
	var err error

	switch format {
	case "yaml":
		envelope := OutputEnvelope{
			APIVersion: APIVersion,
			Kind:       "MembershipList",
			Data:       data,
		}
		out, err = yaml.Marshal(&envelope)
	case "json":
		envelope := OutputEnvelope{
			APIVersion: APIVersion,
			Kind:       "MembershipList",
			Data:       data,
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		err = enc.Encode(&envelope)
		out = buf.Bytes()
	case "name":
		var sb strings.Builder
		for _, m := range data.Items {
			fmt.Fprintln(&sb, m.Name)
		}
		out = []byte(sb.String())
	case "wide":
		out = formatTable(data.Items, true)
	default:
		out = formatTable(data.Items, false)
	}

	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}

	if outputFile != "" {
		if err := os.WriteFile(outputFile, out, 0644); err != nil {
			return fmt.Errorf("write %s: %w", outputFile, err)
		}
		logHuman("Output written to %s\n", outputFile)
		return nil
	}
	_, err = os.Stdout.Write(out)
	return err
}

func formatTable(items []Assignment, wide bool) []byte {
	var entitlements, groups []Assignment
	for _, m := range items {
		if m.Kind == "Group" {
			groups = append(groups, m)
		} else {
			entitlements = append(entitlements, m)
		}
	}

	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)

	if wide {
		_, _ = fmt.Fprintln(w, "NAME\tID\tROLE\tACCOUNT\tEXPIRES\tSTATUS")
	} else {
		_, _ = fmt.Fprintln(w, "NAME\tID\tROLE\tEXPIRES\tSTATUS")
	}

	for _, m := range entitlements {
		writeTableRow(w, m, wide)
	}
	_ = w.Flush()

	if len(groups) > 0 {
		sb.WriteString("\nGroups:\n")
		gw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(gw, "NAME\tEXPIRES")
		for _, g := range groups {
			_, _ = fmt.Fprintf(gw, "%s\t%s\n", g.Name, shortTimestamp(g.ExpirationDate))
		}
		_ = gw.Flush()
	}

	return []byte(sb.String())
}

func writeTableRow(w *tabwriter.Writer, m Assignment, wide bool) {
	status := "ok"
	if m.Expiring {
		status = "expiring"
	}
	expires := shortTimestamp(m.ExpirationDate)
	if wide {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			m.Name, m.ID, m.Role, m.Account, expires, status)
	} else {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.Name, m.ID, m.Role, expires, status)
	}
}

func shortTimestamp(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02 15:04")
}

func printCachedGet(format, outputFile string, args []string) error {
	cacheDir := cacheDirectory()
	membershipsPath := filepath.Join(cacheDir, "memberships-cache.yaml")
	detailsPath := filepath.Join(cacheDir, "details-cache.yaml")

	var assignments []Assignment
	if data, err := os.ReadFile(membershipsPath); err == nil {
		_ = yaml.Unmarshal(data, &assignments)
	}

	var details []Resource
	if data, err := os.ReadFile(detailsPath); err == nil {
		_ = yaml.Unmarshal(data, &details)
	}

	if len(assignments) == 0 {
		logHuman("No cached membership data. Run 'authzer get' with browser access first.\n")
		return nil
	}

	if len(args) > 0 {
		assignments = filterAssignments(assignments, args)
		details = filterDetailsByArgs(details, args)
	}

	getdata := GetData{
		Updated:    "(cached)",
		TotalItems: len(assignments),
		Items:      assignments,
		Details:    details,
	}
	return printGetOutput(getdata, format, outputFile)
}

func cacheDirectory() string {
	configFile := viper.ConfigFileUsed()
	if configFile != "" {
		return filepath.Dir(configFile)
	}
	return filepath.Join(xdgConfigHome(), "authzer")
}

func cacheFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeCache(path string, details []Resource) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(details)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readCache(path string) ([]Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var details []Resource
	if err := yaml.Unmarshal(data, &details); err != nil {
		return nil, err
	}
	return details, nil
}

func printComplianceSummary(memberships []Assignment, details []Resource) {
	group, groupErr := requireGroup()
	if groupErr != nil {
		return
	}
	rules, _, resolveErr := resolveRulesForGroup(group)
	if resolveErr != nil {
		return
	}

	nameByID := buildNameLookup(details)

	membershipNames := make(map[string]bool, len(memberships))
	for _, m := range memberships {
		membershipNames[m.Name] = true
	}

	ruleNames := make(map[string]bool, len(rules))
	for _, r := range rules {
		name := nameByID[r.Resource]
		if name == "" {
			name = r.Resource
		}
		ruleNames[name] = true
	}

	var managed, undeclared int
	for _, m := range memberships {
		if m.Kind == "Group" {
			continue
		}
		if ruleNames[m.Name] {
			managed++
		} else {
			undeclared++
		}
	}

	var missing []string
	for _, r := range rules {
		name := nameByID[r.Resource]
		if name == "" {
			name = r.Resource
		}
		if !membershipNames[name] {
			missing = append(missing, name)
		}
	}

	logHuman("\nPolicy compliance (group: %s):\n", group)
	logHuman("  Managed:    %d\n", managed)
	logHuman("  Undeclared: %d\n", undeclared)
	if len(missing) > 0 {
		logHuman("  Missing:    %d\n", len(missing))
		for _, name := range missing {
			logHuman("    - %s\n", name)
		}
	} else {
		logHuman("  Missing:    0 (all policy resources present)\n")
	}
	logHuman("\n")
}

func filterAssignments(assignments []Assignment, args []string) []Assignment {
	var out []Assignment
	for _, m := range assignments {
		for _, arg := range args {
			if strings.EqualFold(m.Name, arg) || strings.EqualFold(m.ID, arg) {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

func filterDetailsByArgs(details []Resource, args []string) []Resource {
	var out []Resource
	for _, d := range details {
		for _, arg := range args {
			if strings.EqualFold(d.Name, arg) || strings.EqualFold(d.ID, arg) {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

func writeMembershipsCache(path string, memberships []Assignment) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(memberships)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
