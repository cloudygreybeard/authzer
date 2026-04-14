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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage authzer configuration and contexts",
	Long: `View and modify authzer configuration. Subcommands manage named
contexts, each representing a self-contained portal configuration
directory with its own config, policy, scripts, and cache.

Available subcommands:

  list      List registered contexts
  current   Print the active context name
  use       Set the active context
  view      Show resolved configuration
  policy    Show RBAC policy summary for the active group
  import    Import a SitePack manifest as a new context`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered contexts",
	RunE:  runConfigList,
}

var configCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Print the active context name",
	RunE:  runConfigCurrent,
}

var configUseCmd = &cobra.Command{
	Use:   "use NAME",
	Short: "Set the active context",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigUse,
}

var configViewCmd = &cobra.Command{
	Use:   "view [CONTEXT]",
	Short: "Show resolved configuration for a context",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runConfigView,
}

var configPolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Show RBAC policy summary for the active group",
	Long: `Display a synopsis of the RBAC policy resolved for the active group.
Shows the group identity, justification text, bound roles, and the
flattened rule set.

With -o yaml or -o json, emits the full policy manifests as structured
output suitable for piping into yq or jq.`,
	RunE: runConfigPolicy,
}

func init() {
	configPolicyCmd.Flags().StringP("output", "o", "", `output format: "yaml", "json"`)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configCurrentCmd)
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configPolicyCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigList(_ *cobra.Command, _ []string) error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil || len(reg.Contexts) == 0 {
		fmt.Fprintln(os.Stderr, "No contexts registered. Run: authzer config import -f MANIFEST")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CURRENT\tNAME\tPATH")
	for _, entry := range reg.Contexts {
		marker := ""
		if entry.Name == reg.CurrentContext {
			marker = "*"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", marker, entry.Name, entry.Path)
	}
	return w.Flush()
}

func runConfigCurrent(_ *cobra.Command, _ []string) error {
	if activeContext != "" {
		fmt.Println(activeContext)
		return nil
	}

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil || reg.CurrentContext == "" {
		return fmt.Errorf("no current context; run: authzer config import -f MANIFEST")
	}
	fmt.Println(reg.CurrentContext)
	return nil
}

func runConfigUse(_ *cobra.Command, args []string) error {
	name := args[0]

	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("no contexts registered; run: authzer config import -f MANIFEST")
	}

	found := false
	for _, entry := range reg.Contexts {
		if entry.Name == name {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(reg.Contexts))
		for _, e := range reg.Contexts {
			names = append(names, e.Name)
		}
		return fmt.Errorf("context %q not found; available: %v", name, names)
	}

	reg.CurrentContext = name
	if err := saveRegistry(reg); err != nil {
		return err
	}
	logHuman("Switched to context %q.\n", name)
	return nil
}

func runConfigView(cmd *cobra.Command, args []string) error {
	ctxName := activeContext
	if len(args) > 0 {
		ctxName = args[0]
	}

	if ctxName != "" {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		if reg != nil {
			dir, err := resolveContextDir(reg, ctxName)
			if err != nil {
				return err
			}
			configPath := filepath.Join(dir, "config.yaml")
			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("reading config: %w", err)
			}
			logHuman("# Context: %s\n# Config:  %s\n", ctxName, configPath)
			fmt.Print(string(data))
			return nil
		}
	}

	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		return fmt.Errorf("no config file loaded")
	}

	allSettings := viper.AllSettings()
	data, err := yaml.Marshal(allSettings)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	logHuman("# Config: %s\n", configFile)
	fmt.Print(string(data))
	return nil
}

func runConfigPolicy(cmd *cobra.Command, _ []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")

	policy, err := loadPolicy()
	if err != nil {
		return err
	}

	if outputFormat != "" {
		return printPolicyStructured(policy, outputFormat)
	}

	policyRef := viper.GetString("policy")
	if policyRef == "" {
		policyRef = "(none)"
	}

	group := viper.GetString("group")
	if group == "" {
		return printPolicyInventory(policy, policyRef)
	}

	rules, justification, err := policy.Resolve(group)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Policy:  %s\n", policyRef)
	fmt.Fprintf(os.Stdout, "Group:   %s\n", group)
	fmt.Fprintf(os.Stdout, "Justify: %s\n", justification)

	var roleNames []string
	for _, rb := range policy.RoleBindings {
		for _, subj := range rb.Subjects {
			if subj.Kind == "Group" && subj.Name == group {
				roleNames = append(roleNames, rb.RoleRef.Name)
				break
			}
		}
	}

	fmt.Fprintf(os.Stdout, "\nRoles (via %d RoleBinding", len(roleNames))
	if len(roleNames) != 1 {
		fmt.Fprint(os.Stdout, "s")
	}
	fmt.Fprintln(os.Stdout, "):")
	for _, rn := range roleNames {
		role := policy.Roles[rn]
		n := len(role.Rules)
		unit := "rules"
		if n == 1 {
			unit = "rule"
		}
		fmt.Fprintf(os.Stdout, "  %-20s %d %s\n", rn, n, unit)
	}

	fmt.Fprintf(os.Stdout, "\nRules (%d total):\n", len(rules))
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "  RESOURCE\tPERMISSION")
	for _, r := range rules {
		fmt.Fprintf(w, "  %s\t%s\n", r.Resource, r.Permission)
	}
	return w.Flush()
}

func printPolicyInventory(policy *Policy, policyRef string) error {
	fmt.Fprintf(os.Stdout, "Policy: %s\n", policyRef)
	fmt.Fprintf(os.Stdout, "Group:  (not set)\n")

	fmt.Fprintf(os.Stdout, "\nGroups (%d):\n", len(policy.Groups))
	groupNames := make([]string, 0, len(policy.Groups))
	for name := range policy.Groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, name := range groupNames {
		fmt.Fprintf(os.Stdout, "  %s\n", name)
	}

	fmt.Fprintf(os.Stdout, "\nRoles (%d):\n", len(policy.Roles))
	rNames := make([]string, 0, len(policy.Roles))
	for name := range policy.Roles {
		rNames = append(rNames, name)
	}
	sort.Strings(rNames)
	for _, name := range rNames {
		n := len(policy.Roles[name].Rules)
		unit := "rules"
		if n == 1 {
			unit = "rule"
		}
		fmt.Fprintf(os.Stdout, "  %-20s %d %s\n", name, n, unit)
	}

	fmt.Fprintf(os.Stdout, "\nRoleBindings (%d):\n", len(policy.RoleBindings))
	for _, rb := range policy.RoleBindings {
		fmt.Fprintf(os.Stdout, "  %s\n", rb.Metadata.Name)
	}

	logHuman("\nHint: set --group or group in config.yaml to see resolved rules.\n")
	return nil
}

type policyData struct {
	Groups       []Group       `yaml:"groups" json:"groups"`
	Roles        []Role        `yaml:"roles" json:"roles"`
	RoleBindings []RoleBinding `yaml:"roleBindings" json:"roleBindings"`
}

func printPolicyStructured(policy *Policy, format string) error {
	data := policyData{}
	for _, g := range policy.Groups {
		data.Groups = append(data.Groups, *g)
	}
	for _, r := range policy.Roles {
		data.Roles = append(data.Roles, *r)
	}
	for _, rb := range policy.RoleBindings {
		data.RoleBindings = append(data.RoleBindings, *rb)
	}

	envelope := OutputEnvelope{
		APIVersion: APIVersion,
		Kind:       "Policy",
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
		return fmt.Errorf("marshalling policy: %w", err)
	}
	_, err = os.Stdout.Write(out)
	return err
}
