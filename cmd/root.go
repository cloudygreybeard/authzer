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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Version information set via ldflags at build time.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "authzer",
	Short: "Declarative access management, even if the only API is a button",
	Long: `Declarative access management, even if the only API is a button.

authzer connects to a running browser via Chrome DevTools Protocol (CDP),
queries membership status, and reconciles against RBAC policy by renewing
expiring memberships or requesting new ones.

All portal-specific details come from a YAML config file.`,
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		return initConfig(cmd)
	},
}

// DryRun modes control what level of interaction authzer performs.
const (
	DryRunClient = "client" // local only: resolve policy, no CDP
	DryRunServer = "server" // CDP connected, read-only: prepare forms but do not submit, leave tabs open
	DryRunNone   = "none"   // full execution: submit forms, close tabs
)

var flagContext string

func init() {
	rootCmd.PersistentFlags().StringVar(&flagContext, "context", "", "override active context (env: AUTHZER_CONTEXT)")
	rootCmd.PersistentFlags().String("cdp", "", "CDP HTTP endpoint")
	rootCmd.PersistentFlags().String("group", "", "organisational role group (e.g. sre, senior-sre)")
	rootCmd.PersistentFlags().String("justification", "", "override group's default justification text")
	rootCmd.PersistentFlags().IntP("concurrency", "j", 0, "concurrent browser tabs")
	rootCmd.PersistentFlags().String("dry-run", "server",
		`dry-run mode: "client" (local policy only, no browser), "server" (browser connected, `+
			`prepare forms but do not submit), "none" (full execution)`)
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose/debug output (shorthand for --log-level=debug --log-file=stderr)")
	rootCmd.PersistentFlags().String("log-file", "", `structured JSONL log destination (path, "-" for stdout, "stderr" for stderr)`)
	rootCmd.PersistentFlags().String("log-level", "info", `minimum log level: debug, info, warn, error`)
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "suppress human-readable stderr output")
}

// cdpURL returns the CDP HTTP endpoint, derived from the --cdp flag if
// set, or composed from browser.address and browser.port.
func cdpURL() string {
	if v := viper.GetString("cdp"); v != "" {
		return v
	}
	return fmt.Sprintf("http://%s:%d", viper.GetString("browser.address"), viper.GetInt("browser.port"))
}

// browserProfileDir returns the directory to use for --user-data-dir.
// If browser.userDataDir is configured, it is returned as-is. Otherwise
// a default under the XDG config directory is used.
func browserProfileDir() string {
	if v := viper.GetString("browser.userDataDir"); v != "" {
		return v
	}
	return filepath.Join(xdgConfigHome(), "authzer", "browser-profile")
}

// dryRunMode returns the current dry-run mode from config/flags.
func dryRunMode() string {
	mode := viper.GetString("dry-run")
	switch mode {
	case DryRunClient, DryRunServer, DryRunNone:
		return mode
	default:
		return DryRunServer
	}
}

// requireGroup returns the configured group name, or an error with
// guidance if no group has been set via flag, env, or config.
func requireGroup() (string, error) {
	g := viper.GetString("group")
	if g == "" {
		return "", fmt.Errorf("no group configured (set --group, AUTHZER_GROUP, or group in config.yaml)")
	}
	return g, nil
}

// allURLArgs returns true if every argument is a URL (http:// or https://),
// meaning no RBAC policy resolution is needed.
func allURLArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "http://") && !strings.HasPrefix(a, "https://") {
			return false
		}
	}
	return true
}

// filterRules narrows a resolved rule set to only those matching the
// given positional arguments. Each arg is matched against Rule.Resource
// (slug ID) and the display name from the details cache
// (case-insensitive). If an arg looks like a URL it is treated as an
// ad-hoc rule with no policy context. Returns an error if any arg
// matched nothing.
func filterRules(rules []Rule, args []string) ([]Rule, error) {
	if len(args) == 0 {
		return rules, nil
	}

	cacheDir := cacheDirectory()
	var details []Resource
	if data, readErr := os.ReadFile(filepath.Join(cacheDir, "details-cache.yaml")); readErr == nil {
		_ = yaml.Unmarshal(data, &details)
	}
	nameByID := make(map[string]string, len(details))
	for _, d := range details {
		if d.ID != "" && d.Name != "" {
			nameByID[d.ID] = d.Name
		}
	}

	byResource := make(map[string]Rule, len(rules))
	byName := make(map[string]Rule, len(rules))
	for _, r := range rules {
		byResource[strings.ToLower(r.Resource)] = r
		if name := nameByID[r.Resource]; name != "" {
			byName[strings.ToLower(name)] = r
		}
	}

	var filtered []Rule
	var missing []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			filtered = append(filtered, Rule{SelfLink: arg})
			continue
		}
		lower := strings.ToLower(arg)
		if r, ok := byResource[lower]; ok {
			filtered = append(filtered, r)
		} else if r, ok := byName[lower]; ok {
			filtered = append(filtered, r)
		} else {
			missing = append(missing, arg)
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("resources not found in policy: %s", strings.Join(missing, ", "))
	}
	return filtered, nil
}

// Execute runs the root command with the given context.
func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}

// xdgConfigHome returns the XDG config directory. It uses
// $XDG_CONFIG_HOME if set, otherwise falls back to ~/.config on all
// platforms (including Windows).
func xdgConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func initConfig(cmd *cobra.Command) error {
	viper.SetConfigType("yaml")
	viper.SetConfigName("config")

	viper.SetEnvPrefix("AUTHZER")
	viper.AutomaticEnv()

	// Resolve context directory before adding config search paths.
	ctxName := flagContext
	if ctxName == "" {
		ctxName = os.Getenv("AUTHZER_CONTEXT")
	}

	if ctxName != "" {
		reg, err := loadRegistry()
		if err != nil {
			return err
		}
		if reg == nil {
			return fmt.Errorf("context %q requested but no contexts registered; run: authzer config import", ctxName)
		}
		dir, err := resolveContextDir(reg, ctxName)
		if err != nil {
			return err
		}
		activeContext = ctxName
		viper.AddConfigPath(dir)
	} else {
		reg, _ := loadRegistry()
		if reg != nil && reg.CurrentContext != "" && len(reg.Contexts) > 0 {
			dir, err := resolveContextDir(reg, reg.CurrentContext)
			if err != nil {
				return err
			}
			activeContext = reg.CurrentContext
			viper.AddConfigPath(dir)
		} else {
			viper.AddConfigPath(filepath.Join(xdgConfigHome(), "authzer"))
		}
	}
	viper.AddConfigPath(".")

	viper.SetDefault("concurrency", 3)
	viper.SetDefault("settleDelay", "1s")
	viper.SetDefault("verbose", false)

	viper.SetDefault("policy", "policy.yaml")
	viper.SetDefault("renewWithinDays", 30)

	viper.SetDefault("survey.timeout", "120s")

	viper.SetDefault("browser.port", 9222)
	viper.SetDefault("browser.address", "127.0.0.1")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config: %w", err)
		}
	} else {
		if v := viper.GetString("apiVersion"); v != "" && v != APIVersion {
			return fmt.Errorf("unsupported config apiVersion %q (expected %s)", v, APIVersion)
		}
		if viper.GetBool("verbose") {
			logHuman("Using config: %s\n", viper.ConfigFileUsed())
		}
	}

	_ = viper.BindPFlag("cdp", cmd.Flags().Lookup("cdp"))
	_ = viper.BindPFlag("concurrency", cmd.Flags().Lookup("concurrency"))
	_ = viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("justification", cmd.Flags().Lookup("justification"))
	_ = viper.BindPFlag("verbose", cmd.Flags().Lookup("verbose"))
	_ = viper.BindPFlag("group", cmd.Flags().Lookup("group"))
	_ = viper.BindPFlag("log.file", cmd.Flags().Lookup("log-file"))
	_ = viper.BindPFlag("log.level", cmd.Flags().Lookup("log-level"))
	_ = viper.BindPFlag("quiet", cmd.Flags().Lookup("quiet"))

	quiet = viper.GetBool("quiet")

	if err := initAuditLog(); err != nil {
		return err
	}

	return nil
}

// logFile holds a reference to the opened log file so it can be closed
// on process exit. Nil when logging to stdout/stderr/discard.
var logFile *os.File

func initAuditLog() error {
	logLevel := ParseLevel(viper.GetString("log.level"))
	logDest := viper.GetString("log.file")

	// --verbose enables debug-level human output ([verbose] lines on
	// stderr) for backward compatibility. It does not emit JSONL unless
	// --log-file is also set.
	verboseOnly := viper.GetBool("verbose") && logDest == ""
	if viper.GetBool("verbose") {
		logLevel = LevelDebug
	}

	if logDest == "" && !verboseOnly {
		auditLog = NewLogger(nil, LevelInfo)
		return nil
	}
	if verboseOnly {
		auditLog = NewLogger(nil, LevelDebug)
		return nil
	}

	var w io.Writer
	switch logDest {
	case "-":
		w = os.Stdout
	case "stderr":
		w = os.Stderr
	default:
		f, err := os.OpenFile(logDest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("opening log file %s: %w", logDest, err)
		}
		logFile = f
		w = f
	}
	auditLog = NewLogger(w, logLevel)
	return nil
}
