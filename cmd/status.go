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
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// StatusData is the output payload for the status command.
type StatusData struct {
	Group      string        `yaml:"group" json:"group"`
	TotalRules int           `yaml:"totalRules" json:"totalRules"`
	Satisfied  int           `yaml:"satisfied" json:"satisfied"`
	Missing    int           `yaml:"missing" json:"missing"`
	Errors     int           `yaml:"errors" json:"errors"`
	ByKind     []KindSummary `yaml:"byKind" json:"byKind"`
	Items      []StatusItem  `yaml:"items" json:"items"`
}

// KindSummary aggregates status by provider kind.
type KindSummary struct {
	Kind      string `yaml:"kind" json:"kind"`
	Provider  string `yaml:"provider" json:"provider"`
	Total     int    `yaml:"total" json:"total"`
	Satisfied int    `yaml:"satisfied" json:"satisfied"`
	Missing   int    `yaml:"missing" json:"missing"`
	Errors    int    `yaml:"errors" json:"errors"`
}

// StatusItem is the per-rule check result.
type StatusItem struct {
	Kind     string `yaml:"kind" json:"kind"`
	Resource string `yaml:"resource" json:"resource"`
	Status   string `yaml:"status" json:"status"`
	Message  string `yaml:"message,omitempty" json:"message,omitempty"`
	Provider string `yaml:"provider" json:"provider"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check compliance of all policy rules across providers",
	Long: `Evaluate every rule in the RBAC policy against the appropriate
provider and report compliance status.

Rules with kind "Entitlement" are checked against cached portal data
(run 'authzer get' first). Rules with kind "EntraGroup" or
"AzureRoleAssignment" are checked via the Azure CLI in real-time.

Providers must be enabled in config to be queried:

  providers:
    entra:
      enabled: true
    azureRBAC:
      enabled: true
      scope: /subscriptions/SUBSCRIPTION_ID   # optional

Output format:
  (default)   Tabular summary
  -o yaml     Full YAML
  -o json     Full JSON`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().StringP("output", "o", "", `output format: "yaml", "json"`)
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	outputFormat, _ := cmd.Flags().GetString("output")

	group, err := requireGroup()
	if err != nil {
		return err
	}

	rules, _, err := resolveRulesForGroup(group)
	if err != nil {
		return err
	}

	reg := initRegistry()
	byKind := RulesByKind(rules, "Entitlement")

	logHuman("Checking %d rules across %d provider(s)…\n\n", len(rules), len(byKind))

	var items []StatusItem
	kindSummaries := make(map[string]*KindSummary)

	for kind, kindRules := range byKind {
		provider, provErr := reg.ForKind(kind)
		if provErr != nil {
			for _, r := range kindRules {
				items = append(items, StatusItem{
					Kind:     kind,
					Resource: r.Resource,
					Status:   "error",
					Message:  provErr.Error(),
					Provider: "none",
				})
			}
			summary := getOrCreateSummary(kindSummaries, kind, "none")
			summary.Errors += len(kindRules)
			continue
		}

		checkProviderRules(ctx, kindRules, kind, provider, &items, kindSummaries)
	}

	data := buildStatusData(group, rules, items, kindSummaries)
	return printStatusOutput(data, outputFormat)
}

func checkProviderRules(ctx context.Context, rules []Rule, kind string, provider Provider, items *[]StatusItem, summaries map[string]*KindSummary) {
	summary := getOrCreateSummary(summaries, kind, provider.Name())

	for _, r := range rules {
		result, err := provider.Check(ctx, r)
		if err != nil {
			*items = append(*items, StatusItem{
				Kind:     kind,
				Resource: r.Resource,
				Status:   "error",
				Message:  err.Error(),
				Provider: provider.Name(),
			})
			summary.Errors++
			continue
		}

		if result.Satisfied {
			*items = append(*items, StatusItem{
				Kind:     kind,
				Resource: r.Resource,
				Status:   "satisfied",
				Message:  result.Message,
				Provider: provider.Name(),
			})
			summary.Satisfied++
		} else {
			*items = append(*items, StatusItem{
				Kind:     kind,
				Resource: r.Resource,
				Status:   "missing",
				Message:  result.Message,
				Provider: provider.Name(),
			})
			summary.Missing++
		}
	}
}

func getOrCreateSummary(m map[string]*KindSummary, kind, provider string) *KindSummary {
	if s, ok := m[kind]; ok {
		return s
	}
	s := &KindSummary{Kind: kind, Provider: provider}
	m[kind] = s
	return s
}

func buildStatusData(group string, allRules []Rule, items []StatusItem, summaries map[string]*KindSummary) StatusData {
	var satisfied, missing, errCount int
	for _, item := range items {
		switch item.Status {
		case "satisfied":
			satisfied++
		case "missing":
			missing++
		case "error":
			errCount++
		}
	}

	var byKind []KindSummary
	for _, s := range summaries {
		s.Total = s.Satisfied + s.Missing + s.Errors
		byKind = append(byKind, *s)
	}

	return StatusData{
		Group:      group,
		TotalRules: len(allRules),
		Satisfied:  satisfied,
		Missing:    missing,
		Errors:     errCount,
		ByKind:     byKind,
		Items:      items,
	}
}

func printStatusOutput(data StatusData, format string) error {
	switch format {
	case "yaml":
		envelope := OutputEnvelope{
			APIVersion: APIVersion,
			Kind:       "StatusReport",
			Data:       data,
		}
		out, err := yaml.Marshal(&envelope)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err

	case "json":
		envelope := OutputEnvelope{
			APIVersion: APIVersion,
			Kind:       "StatusReport",
			Data:       data,
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(&envelope); err != nil {
			return err
		}
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}

	logHuman("Policy compliance for group: %s\n\n", data.Group)

	for _, s := range data.ByKind {
		logHuman("  %s (%s): %d/%d satisfied", s.Kind, s.Provider, s.Satisfied, s.Total)
		if s.Missing > 0 {
			logHuman(", %d missing", s.Missing)
		}
		if s.Errors > 0 {
			logHuman(", %d errors", s.Errors)
		}
		logHuman("\n")
	}

	logHuman("\n")

	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tRESOURCE\tSTATUS\tPROVIDER\tMESSAGE")
	for _, item := range data.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			item.Kind, item.Resource, item.Status, item.Provider, item.Message)
	}
	_ = w.Flush()
	_, err := os.Stdout.Write([]byte(sb.String()))
	return err
}

func readMembershipsCache(cacheDir string) ([]Assignment, error) {
	path := fmt.Sprintf("%s/memberships-cache.yaml", cacheDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var assignments []Assignment
	if err := yaml.Unmarshal(data, &assignments); err != nil {
		return nil, err
	}
	return assignments, nil
}
