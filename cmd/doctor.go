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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check authzer setup and diagnose configuration issues",
	Long: `Validate the authzer environment by checking configuration files,
RBAC policy, browser availability, CDP connectivity, and script
references. Reports each check as ok, warning, or error with
actionable guidance.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	issues := 0

	// Config file
	configFile := viper.ConfigFileUsed()
	if configFile != "" {
		printCheck("Config file", configFile, "ok", "")
	} else {
		printCheck("Config file", "not found", "warning", "create config.yaml or ~/.config/authzer/config.yaml")
		issues++
	}

	// Policy file
	policy, policyErr := loadPolicy()
	if policyErr != nil {
		printCheck("Policy file", "", "error", policyErr.Error())
		issues++
	} else {
		policyPath := viper.GetString("policy")
		summary := fmt.Sprintf("%d roles, %d groups, %d bindings",
			len(policy.Roles), len(policy.Groups), len(policy.RoleBindings))
		printCheck("Policy file", policyPath, "ok", summary)
	}

	// Group
	group, groupErr := requireGroup()
	if groupErr != nil {
		printCheck("Group", "", "warning", "not configured (set --group, AUTHZER_GROUP, or group in config.yaml)")
		issues++
	} else {
		if policy != nil {
			_, _, resolveErr := policy.Resolve(group)
			if resolveErr != nil {
				printCheck("Group", group, "error", resolveErr.Error())
				issues++
			} else {
				printCheck("Group", group, "ok", "resolvable in policy")
			}
		} else {
			printCheck("Group", group, "ok", "set (policy not loaded to verify)")
		}
	}

	// Browser binary
	browserPath := viper.GetString("browser.path")
	if browserPath != "" {
		if _, err := os.Stat(browserPath); err == nil {
			printCheck("Browser", browserPath, "ok", "configured")
		} else {
			printCheck("Browser", browserPath, "error", "configured path not found")
			issues++
		}
	} else {
		detected := findBrowserPath()
		if detected != "" {
			printCheck("Browser", detected, "ok", "auto-detected")
		} else {
			printCheck("Browser", "", "warning", "not found; set browser.path in config")
			issues++
		}
	}

	// Profile directory
	profileDir := browserProfileDir()
	if info, err := os.Stat(profileDir); err == nil && info.IsDir() {
		printCheck("Profile dir", profileDir, "ok", "exists")
	} else {
		printCheck("Profile dir", profileDir, "info", "will be created on first authzer launch")
	}

	// CDP endpoint
	endpoint := cdpURL()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(endpoint + "/json/version")
	if err == nil && resp.StatusCode == http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		var version struct {
			Browser  string `json:"Browser"`
			Protocol string `json:"Protocol-Version"`
		}
		if jsonErr := json.NewDecoder(resp.Body).Decode(&version); jsonErr == nil {
			detail := version.Browser
			if version.Protocol != "" {
				detail += fmt.Sprintf(" (protocol %s)", version.Protocol)
			}
			printCheck("CDP endpoint", endpoint, "ok", detail)
		} else {
			printCheck("CDP endpoint", endpoint, "ok", "reachable")
		}
	} else {
		printCheck("CDP endpoint", endpoint, "error", "not reachable; run: authzer launch")
		issues++
	}

	// Script files
	scriptKeys := []string{
		"portal.page.infoJs",
		"portal.form.infoJs",
		"portal.findButtonJs",
		"portal.formReadyJs",
		"portal.findCloseJs",
		"portal.form.selectPermissionJs",
		"portal.form.fillJustificationJs",
		"portal.form.checkTermsJs",
		"portal.memberships.listJs",
		"portal.memberships.selectJs",
	}
	found, missing := 0, 0
	for _, key := range scriptKeys {
		if _, err := resolveScript(key); err == nil {
			found++
		} else {
			missing++
		}
	}
	if configFile != "" {
		if missing == 0 && found > 0 {
			printCheck("Scripts", fmt.Sprintf("%d/%d", found, found+missing), "ok", "all referenced scripts found")
		} else if found > 0 {
			printCheck("Scripts", fmt.Sprintf("%d/%d", found, found+missing), "warning",
				fmt.Sprintf("%d script(s) missing or not configured", missing))
			issues++
		} else if missing > 0 {
			printCheck("Scripts", fmt.Sprintf("0/%d", missing), "warning", "no portal scripts configured")
		} else {
			printCheck("Scripts", "", "info", "no portal scripts configured")
		}
	}

	fmt.Fprintln(os.Stderr)
	if issues == 0 {
		fmt.Fprintln(os.Stderr, "All checks passed.")
	} else {
		fmt.Fprintf(os.Stderr, "%d issue(s) found.\n", issues)
	}
	return nil
}

func printCheck(name, value, status, detail string) {
	var icon string
	switch status {
	case "ok":
		icon = "ok"
	case "warning":
		icon = "!!"
	case "error":
		icon = "FAIL"
	default:
		icon = "--"
	}

	if value != "" {
		fmt.Fprintf(os.Stderr, "  %-14s %-44s [%s]", name, value, icon)
	} else {
		fmt.Fprintf(os.Stderr, "  %-14s %-44s [%s]", name, "", icon)
	}
	if detail != "" {
		fmt.Fprintf(os.Stderr, "  %s", detail)
	}
	fmt.Fprintln(os.Stderr)
}
