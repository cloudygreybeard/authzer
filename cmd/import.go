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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a SitePack manifest as a new context",
	Long: `Import a SitePack manifest (kind: SitePack) containing templates,
scripts, and value definitions. Templates are rendered with user-supplied
values and written to a named context directory alongside verbatim data
files. The context is registered and set as current.

The context name defaults to metadata.name from the manifest and can be
overridden with --context.

When --values is not provided, the command prompts interactively for
each value defined in the manifest. Supplied values are saved to the
context directory for future re-imports.

Example:

  authzer config import -f site-pack.yaml
  authzer config import -f site-pack.yaml --values values.yaml
  authzer config import -f site-pack.yaml --context staging`,
	RunE: runImport,
}

func init() {
	configImportCmd.Flags().StringP("file", "f", "", "path to SitePack manifest (required)")
	_ = configImportCmd.MarkFlagRequired("file")
	configImportCmd.Flags().String("values", "", "path to values file (skips interactive prompts)")
	configImportCmd.Flags().String("context", "", "context name (defaults to manifest metadata.name)")
	configCmd.AddCommand(configImportCmd)
}

func runImport(cmd *cobra.Command, _ []string) error {
	manifestPath, _ := cmd.Flags().GetString("file")
	valuesPath, _ := cmd.Flags().GetString("values")
	ctxOverride, _ := cmd.Flags().GetString("context")

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	var sp SitePack
	if err := yaml.Unmarshal(raw, &sp); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	if sp.Kind != "SitePack" {
		return fmt.Errorf("expected kind: SitePack, got %q", sp.Kind)
	}
	if sp.APIVersion != "" && sp.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q (expected %s)", sp.APIVersion, APIVersion)
	}

	ctxName := ctxOverride
	if ctxName == "" {
		ctxName = sp.Metadata.Name
	}
	if ctxName == "" {
		ctxName = strings.TrimSuffix(filepath.Base(manifestPath), filepath.Ext(manifestPath))
	}

	fmt.Fprintf(os.Stderr, "Importing SitePack: %s (context: %s)\n", sp.Metadata.Name, ctxName)
	if desc := sp.Metadata.Annotations["description"]; desc != "" {
		fmt.Fprintf(os.Stderr, "  %s\n", desc)
	}
	fmt.Fprintln(os.Stderr)

	vals, err := resolveValues(sp.Values, valuesPath)
	if err != nil {
		return err
	}

	destDir := filepath.Join(xdgConfigHome(), "authzer", ctxName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating context directory: %w", err)
	}

	rendered := 0
	tplNames := sortedKeys(sp.Templates)
	for _, filename := range tplNames {
		content := sp.Templates[filename]
		out, err := renderTemplate(filename, content, vals)
		if err != nil {
			return fmt.Errorf("rendering template %s: %w", filename, err)
		}
		outPath := filepath.Join(destDir, filename)
		if err := writeFileWithDirs(outPath, []byte(out)); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		fmt.Fprintf(os.Stderr, "  rendered  %s\n", filename)
		rendered++
	}

	written := 0
	dataNames := sortedKeys(sp.Data)
	for _, filename := range dataNames {
		content := sp.Data[filename]
		outPath := filepath.Join(destDir, filename)
		if err := writeFileWithDirs(outPath, []byte(content)); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		written++
	}
	if written > 0 {
		fmt.Fprintf(os.Stderr, "  copied    %d data files\n", written)
	}

	valsDst := filepath.Join(destDir, "values.yaml")
	valsData, err := yaml.Marshal(vals)
	if err != nil {
		return fmt.Errorf("marshaling values: %w", err)
	}
	if err := os.WriteFile(valsDst, valsData, 0644); err != nil {
		return fmt.Errorf("writing values: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  saved     values.yaml\n")

	isFirst := false
	if reg, _ := loadRegistry(); reg == nil || len(reg.Contexts) == 0 {
		isFirst = true
	}
	if err := registerContext(ctxName, ctxName, isFirst); err != nil {
		return fmt.Errorf("registering context: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  registered context %q\n", ctxName)

	fmt.Fprintf(os.Stderr, "\nImported to %s\n", destDir)
	fmt.Fprintf(os.Stderr, "  %d templates rendered, %d data files copied\n", rendered, written)
	if isFirst {
		fmt.Fprintf(os.Stderr, "  current context set to %q\n", ctxName)
	}
	fmt.Fprintf(os.Stderr, "\nNext steps:\n")
	fmt.Fprintf(os.Stderr, "  authzer doctor    # validate setup\n")
	fmt.Fprintf(os.Stderr, "  authzer launch    # start browser with CDP\n")
	fmt.Fprintf(os.Stderr, "  authzer get       # check memberships\n")
	return nil
}

func resolveValues(defs []SitePackValue, valuesPath string) (map[string]interface{}, error) {
	vals := make(map[string]interface{})

	if valuesPath != "" {
		raw, err := os.ReadFile(valuesPath)
		if err != nil {
			return nil, fmt.Errorf("reading values file: %w", err)
		}
		if err := yaml.Unmarshal(raw, &vals); err != nil {
			return nil, fmt.Errorf("parsing values file: %w", err)
		}
		return vals, nil
	}

	scanner := bufio.NewScanner(os.Stdin)
	for _, v := range defs {
		if v.Default != "" {
			fmt.Fprintf(os.Stderr, "  %s [%s]: ", v.Prompt, v.Default)
		} else {
			fmt.Fprintf(os.Stderr, "  %s: ", v.Prompt)
		}
		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != "" {
				vals[v.Key] = input
			} else if v.Default != "" {
				vals[v.Key] = v.Default
			}
		} else {
			if v.Default != "" {
				vals[v.Key] = v.Default
			}
		}
	}

	return vals, nil
}

func renderTemplate(name, content string, vals map[string]interface{}) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(content)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, vals); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func writeFileWithDirs(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
