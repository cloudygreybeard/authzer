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

func init() {
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configCurrentCmd)
	configCmd.AddCommand(configUseCmd)
	configCmd.AddCommand(configViewCmd)
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
	fmt.Fprintf(os.Stderr, "Switched to context %q.\n", name)
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
			fmt.Fprintf(os.Stderr, "# Context: %s\n# Config:  %s\n", ctxName, configPath)
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
	fmt.Fprintf(os.Stderr, "# Config: %s\n", configFile)
	fmt.Print(string(data))
	return nil
}
