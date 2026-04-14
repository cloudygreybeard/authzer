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
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var launchCmd = &cobra.Command{
	Use:   "launch",
	Short: "Start a browser with CDP remote debugging enabled",
	Long: `Launch a Chromium-based browser (Edge, Chrome, Chromium) configured
for remote debugging via Chrome DevTools Protocol (CDP).

The browser binary, port, bind address, and profile directory are
resolved from config, environment, or auto-detection. A dedicated
profile directory isolates automation sessions from normal browsing.

The browser is started as a detached process and authzer exits once
CDP is confirmed reachable.`,
	RunE: runLaunch,
}

func init() {
	rootCmd.AddCommand(launchCmd)
}

func runLaunch(cmd *cobra.Command, _ []string) error {
	browserPath := viper.GetString("browser.path")
	if browserPath == "" {
		browserPath = findBrowserPath()
	}
	if browserPath == "" {
		return fmt.Errorf("no browser found\n\nSet browser.path in config.yaml or install Edge, Chrome, or Chromium")
	}

	port := viper.GetInt("browser.port")
	addr := viper.GetString("browser.address")
	if addr == "" {
		addr = "127.0.0.1"
	}
	profileDir := browserProfileDir()
	extraArgs := viper.GetStringSlice("browser.extraArgs")

	endpoint := fmt.Sprintf("http://%s:%d", addr, port)
	if err := checkCDP(endpoint); err == nil {
		logHuman("Browser already reachable at %s\n", endpoint)
		return nil
	}

	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return fmt.Errorf("creating profile directory: %w", err)
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--remote-debugging-address=%s", addr),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
	}
	args = append(args, extraArgs...)

	logHuman("Browser:  %s\n", browserPath)
	logHuman("Port:     %d\n", port)
	logHuman("Address:  %s\n", addr)
	logHuman("Profile:  %s\n", profileDir)

	proc := exec.Command(browserPath, args...)
	proc.Stdout = nil
	proc.Stderr = nil
	if err := proc.Start(); err != nil {
		return fmt.Errorf("starting browser: %w", err)
	}

	if err := proc.Process.Release(); err != nil {
		return fmt.Errorf("detaching browser process: %w", err)
	}

	logHuman("\nWaiting for CDP…\n")
	client := &http.Client{Timeout: 2 * time.Second}
	ready := false
	for i := 0; i < 30; i++ {
		resp, err := client.Get(endpoint + "/json/version")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !ready {
		logHuman("Warning: CDP not reachable after 9s at %s\n", endpoint)
		logHuman("The browser may still be starting. Try: authzer doctor\n")
		return nil
	}

	logHuman("CDP ready at %s\n", endpoint)
	return nil
}
