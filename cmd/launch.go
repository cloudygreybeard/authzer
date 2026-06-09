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
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

	// Fail fast if something else is already holding the port. This catches
	// stale SSH tunnels, other browsers without CDP, or leftover processes.
	if ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port)); err != nil {
		return fmt.Errorf("port %d on %s is already in use\n\n"+
			"Something other than a CDP-enabled browser is holding this port.\n"+
			"Common causes:\n"+
			"  - A stale SSH reverse tunnel (ssh -R %d:...)\n"+
			"  - Another browser instance without --remote-debugging-port\n"+
			"  - A previous authzer launch that didn't shut down cleanly\n\n"+
			"Find the process: ss -tlnp 'sport = %d'  (Linux)\n"+
			"                  netstat -ano | findstr :%d  (Windows)\n\n"+
			"Kill it, then retry: authzer launch",
			port, addr, port, port, port)
	} else {
		_ = ln.Close()
	}

	if runtime.GOOS == "linux" && isWSL() {
		logHuman("Warning: running inside WSL\n")
		logHuman("  WSL2 has a separate network namespace. If the browser runs on\n")
		logHuman("  the Windows side, CDP on 127.0.0.1:%d is not reachable from WSL.\n\n", port)
		logHuman("  Options:\n")
		logHuman("    1. Run authzer from Windows PowerShell instead\n")
		logHuman("    2. Forward the port: ssh -L %d:127.0.0.1:%d localhost\n", port, port)
		logHuman("    3. Start Edge from Windows with --remote-debugging-port=%d\n", port)
		logHuman("       and set cdp: \"http://<windows-ip>:%d\" in config.yaml\n\n", port)
	}

	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			logHuman("Warning: no DISPLAY or WAYLAND_DISPLAY set\n")
			logHuman("  The browser needs a graphical display. It may fail to start.\n\n")
		}
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

	// Monitor the process in the background so we can detect early exits.
	procDone := make(chan error, 1)
	go func() {
		procDone <- proc.Wait()
	}()

	logHuman("\nWaiting for CDP…")

	const (
		pollInterval = 500 * time.Millisecond
		maxWait      = 30 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	dots := 0
	for time.Now().Before(deadline) {
		select {
		case err := <-procDone:
			fmt.Fprintln(os.Stderr)
			if err != nil {
				hint := "Check the browser output for errors."
				if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
					hint = "No graphical display available. Start the browser from\n" +
						"a graphical session, or use --headless=new in browser.extraArgs."
				}
				return fmt.Errorf("browser exited immediately: %v\n\n%s", err, hint)
			}
			return fmt.Errorf("browser exited immediately (exit 0)\n\n" +
				"The browser process started but exited right away. This usually\n" +
				"means it handed off to an already-running instance that was not\n" +
				"started with --remote-debugging-port.\n\n" +
				"Close all browser windows and retry, or start the browser\n" +
				"manually with debugging flags. Run: authzer doctor")
		default:
		}

		if err := checkCDP(endpoint); err == nil {
			fmt.Fprintln(os.Stderr)
			logHuman("CDP ready at %s\n", endpoint)
			// Detach now that we've confirmed CDP is up.
			_ = proc.Process.Release()
			return nil
		}

		time.Sleep(pollInterval)
		dots++
		if dots%2 == 0 {
			fmt.Fprint(os.Stderr, ".")
		}
	}

	fmt.Fprintln(os.Stderr)
	logHuman("Warning: CDP not reachable after %s at %s\n", maxWait, endpoint)
	logHuman("The browser may still be starting. Run: authzer doctor\n")

	// Detach the process -- it may still come up.
	_ = proc.Process.Release()
	return nil
}

// isWSL returns true if running inside Windows Subsystem for Linux.
func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}
