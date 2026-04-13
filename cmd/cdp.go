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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/spf13/viper"
)

var wsRun = regexp.MustCompile(`[\s\x{00a0}\x{feff}\p{Z}]+`)
var puaRe = regexp.MustCompile(`[\x{e000}-\x{f8ff}\x{f0000}-\x{ffffd}\x{100000}-\x{10fffd}]`)

// cleanText strips Private Use Area glyphs (icon font artifacts from
// component frameworks), collapses whitespace runs (including NBSP, BOM,
// and Unicode separators) into a single ASCII space, and trims.
func cleanText(s string) string {
	s = puaRe.ReplaceAllString(s, "")
	return strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
}

func cleanTexts(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if c := cleanText(s); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// Coords holds pixel coordinates and debug metadata returned by JS
// button-finder expressions.
type Coords struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Tag  string  `json:"tag,omitempty"`
	Role string  `json:"role,omitempty"`
	W    int     `json:"w,omitempty"`
	H    int     `json:"h,omitempty"`
}

// pageInfoRaw mirrors the JSON shape returned by portal.page.infoJs.
type pageInfoRaw struct {
	Name                string   `json:"name"`
	ID                  string   `json:"id"`
	Status              string   `json:"status"`
	Domains             []string `json:"domains"`
	Description         string   `json:"description"`
	PrimaryOwners       []string `json:"primaryOwners"`
	SecondaryOwners     []string `json:"secondaryOwners"`
	CustomJustification *string  `json:"customJustification"`
	TermsAndConditions  *string  `json:"termsAndConditions"`
}

// formInfoRaw mirrors the JSON shape returned by portal.form.infoJs.
type formInfoRaw struct {
	Account            string   `json:"account"`
	AccountOptions     []string `json:"accountOptions"`
	Roles              []FormOption `json:"roles"`
	HasTermsCheckbox   bool     `json:"hasTermsCheckbox"`
	TermsCheckboxLabel *string  `json:"termsCheckboxLabel"`
	TermsText          *string  `json:"termsText"`
	HasJustification   bool     `json:"hasJustificationField"`
}

type surveyOpts struct {
	SettleDelay time.Duration
	Timeout     time.Duration
	Verbose     bool
}

// resolveScript loads a config value that may be either inline content or a
// file reference. Values prefixed with "@" are treated as paths relative to
// the config file directory and read from disk. Plain values are returned
// as-is for backward compatibility with inline JS in config.
func resolveScript(key string) (string, error) {
	val := viper.GetString(key)
	if val == "" {
		return "", fmt.Errorf("%s not configured", key)
	}
	if !strings.HasPrefix(val, "@") {
		return val, nil
	}
	ref := val[1:]
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		return "", fmt.Errorf("%s references file %q but no config file was loaded", key, ref)
	}
	path := filepath.Join(filepath.Dir(configFile), ref)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("loading %s from %s: %w", key, path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// checkCDP verifies that the CDP endpoint is reachable. It tries the
// /json/version HTTP endpoint first (works on Edge and older Chrome),
// then falls back to a direct WebSocket probe (Chrome 136+ with
// --user-data-dir returns 404 on HTTP discovery). If neither succeeds,
// it returns an error with actionable launch guidance.
func checkCDP(cdpURL string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(cdpURL + "/json/version")
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}

	wsURL := strings.Replace(cdpURL, "http://", "ws://", 1) + "/devtools/browser"
	wsProbe, err := http.NewRequest("GET", strings.Replace(wsURL, "ws://", "http://", 1), nil)
	if err == nil {
		wsProbe.Header.Set("Connection", "Upgrade")
		wsProbe.Header.Set("Upgrade", "websocket")
		wsResp, wsErr := client.Do(wsProbe)
		if wsErr == nil {
			_ = wsResp.Body.Close()
			if wsResp.StatusCode == http.StatusSwitchingProtocols || wsResp.StatusCode == http.StatusBadRequest {
				return nil
			}
		}
	}

	return fmt.Errorf("cannot reach CDP endpoint at %s\n\n%s", cdpURL, browserLaunchHint())
}

// browserLaunchHint returns user-facing guidance for launching a browser
// with remote debugging enabled. It uses the configured browser path, port,
// address, and profile directory, falling back to auto-detection.
func browserLaunchHint() string {
	port := fmt.Sprintf("%d", viper.GetInt("browser.port"))
	addr := viper.GetString("browser.address")
	if addr == "" {
		addr = "127.0.0.1"
	}
	profileDir := browserProfileDir()

	browserPath := viper.GetString("browser.path")
	if browserPath == "" {
		browserPath = findBrowserPath()
	}

	var sb strings.Builder
	sb.WriteString("Ensure a Chromium-based browser is running with remote debugging enabled:\n\n")

	if browserPath != "" {
		if strings.Contains(browserPath, " ") {
			browserPath = `"` + browserPath + `"`
		}
		fmt.Fprintf(&sb, "    %s \\\n", browserPath)
	} else {
		sb.WriteString("    <browser> \\\n")
		sb.WriteString("    # e.g. msedge, google-chrome, chromium\n")
	}
	fmt.Fprintf(&sb, "        --remote-debugging-port=%s \\\n", port)
	fmt.Fprintf(&sb, "        --remote-debugging-address=%s \\\n", addr)
	fmt.Fprintf(&sb, "        --user-data-dir=%s\n", profileDir)

	sb.WriteString("\nOr run: authzer launch\n")
	sb.WriteString("\nIf the browser is already running without these flags,\n")
	sb.WriteString("close all windows and relaunch with the flags above.")
	return sb.String()
}

// findBrowserPath searches well-known install locations for Edge or Chrome
// and returns the first match, or empty string if none found.
func findBrowserPath() string {
	var candidates []string

	switch runtime.GOOS {
	case "windows":
		progFiles := os.Getenv("ProgramFiles")
		progFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")
		for _, root := range []string{progFiles, progFilesX86, localAppData} {
			if root == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			)
		}
	case "darwin":
		candidates = []string{
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		candidates = []string{
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// connectBrowser returns a chromedp browser context connected to an existing
// browser at the given CDP WebSocket URL (e.g. ws://127.0.0.1:9222).
func connectBrowser(ctx context.Context, wsURL string, verbose bool) (context.Context, context.CancelFunc) {
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, wsURL)

	var opts []chromedp.ContextOption
	if verbose {
		opts = append(opts, chromedp.WithDebugf(log.New(os.Stderr, "cdp: ", log.Lmicroseconds).Printf))
	}

	browserCtx, browserCancel := chromedp.NewContext(allocCtx, opts...)
	cancel := func() {
		browserCancel()
		allocCancel()
	}
	return browserCtx, cancel
}

// newTab creates a new browser tab and returns its context.
func newTab(browserCtx context.Context) (context.Context, context.CancelFunc) {
	return chromedp.NewContext(browserCtx)
}

// dispatchClick clicks the DOM element at the given CSS-pixel coordinates
// using JavaScript rather than raw Input.dispatchMouseEvent. JS clicks
// dispatch events directly to the element and work on background tabs,
// enabling true parallel tab processing without BringToFront.
func dispatchClick(ctx context.Context, x, y float64) error {
	js := fmt.Sprintf(`(() => {
		const el = document.elementFromPoint(%v, %v);
		if (!el) return 'no element';
		el.click();
		return '';
	})()`, x, y)

	var errMsg string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &errMsg)); err != nil {
		return fmt.Errorf("dispatchClick(%v,%v): %w", x, y, err)
	}
	if errMsg != "" {
		return fmt.Errorf("dispatchClick(%v,%v): %s", x, y, errMsg)
	}
	return nil
}

// findButton evaluates the portal.findButtonJs template to locate a
// clickable element by text substring and returns its centre coordinates.
func findButton(ctx context.Context, text string) (Coords, error) {
	jsTemplate, err := resolveScript("portal.findButtonJs")
	if err != nil {
		return Coords{}, err
	}

	js := fmt.Sprintf(jsTemplate, text)
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		return Coords{}, fmt.Errorf("findButton(%q): %w", text, err)
	}
	if raw == "" {
		return Coords{}, fmt.Errorf("button %q not found", text)
	}
	var c Coords
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Coords{}, fmt.Errorf("findButton(%q) unmarshal: %w", text, err)
	}
	return c, nil
}

// findCloseButton evaluates portal.findCloseJs to locate the dialog
// cancel/close button.
func findCloseButton(ctx context.Context) (Coords, error) {
	js, err := resolveScript("portal.findCloseJs")
	if err != nil {
		return Coords{}, err
	}

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		return Coords{}, fmt.Errorf("findCloseButton: %w", err)
	}
	if raw == "" {
		return Coords{}, fmt.Errorf("close button not found")
	}
	var c Coords
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Coords{}, fmt.Errorf("findCloseButton unmarshal: %w", err)
	}
	return c, nil
}

// renewOpts holds options for the renewal CDP flow.
type renewOpts struct {
	SettleDelay   time.Duration
	Timeout       time.Duration
	Verbose       bool
	Justification string
	Permission    string
	DryRun        string
	AcceptTerms   bool
}

// selectPermission evaluates the portal.form.selectPermissionJs script to
// click the radio button matching the desired permission name within the
// open dialog. The script receives the permission name as a JSON-encoded
// string via fmt.Sprintf.
func selectPermission(ctx context.Context, permission string) error {
	scriptTpl, err := resolveScript("portal.form.selectPermissionJs")
	if err != nil {
		return fmt.Errorf("selectPermission config: %w", err)
	}

	permJSON, _ := json.Marshal(permission)
	js := fmt.Sprintf(scriptTpl, string(permJSON))

	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &found)); err != nil {
		return fmt.Errorf("selectPermission(%q): %w", permission, err)
	}
	if !found {
		return fmt.Errorf("permission %q not found in form", permission)
	}
	return nil
}

// fillJustification evaluates the portal.form.fillJustificationJs script
// to locate, focus, and populate the business justification textarea. The
// script receives the justification text as a JSON-encoded string via
// fmt.Sprintf and uses document.execCommand('insertText') to insert text
// through the browser's editing pipeline, triggering native input events.
func fillJustification(ctx context.Context, text string) error {
	scriptTpl, err := resolveScript("portal.form.fillJustificationJs")
	if err != nil {
		return fmt.Errorf("fillJustification config: %w", err)
	}

	textJSON, _ := json.Marshal(text)
	js := fmt.Sprintf(scriptTpl, string(textJSON))

	var ok bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok)); err != nil {
		return fmt.Errorf("fillJustification: %w", err)
	}
	if !ok {
		return fmt.Errorf("justification textarea not found or text not inserted")
	}
	return nil
}

// checkTerms evaluates the portal.form.checkTermsJs script to locate
// and click the terms and conditions checkbox if present.
func checkTerms(ctx context.Context) error {
	scriptTpl, err := resolveScript("portal.form.checkTermsJs")
	if err != nil {
		return fmt.Errorf("checkTerms config: %w", err)
	}

	var found bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(scriptTpl, &found)); err != nil {
		return fmt.Errorf("checkTerms: %w", err)
	}
	return nil
}

// renewResource navigates to a resource URL, opens the request dialog,
// fills the form (permission, justification, T&Cs), and optionally
// submits. In server dry-run mode, tabs are left open on both success
// and failure so the user can inspect the portal state.
func renewResource(ctx context.Context, url string, kind string, opts renewOpts) Resource {
	res := Resource{Kind: kind, SelfLink: url}
	logf := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "  [verbose] "+format+"\n", args...)
		}
	}

	leaveOpen := opts.DryRun == DryRunServer

	parentCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	readySelector := viper.GetString("portal.page.readySelector")
	triggerText := viper.GetString("portal.dialog.triggerText")
	dialogReadySelector := viper.GetString("portal.dialog.readySelector")
	optionText := viper.GetString("portal.dialog.optionText")

	pageInfoJS, err := resolveScript("portal.page.infoJs")
	if err != nil {
		res.Error = fmt.Sprintf("config: %v", err)
		return res
	}
	formReadyJS, err := resolveScript("portal.formReadyJs")
	if err != nil {
		res.Error = fmt.Sprintf("config: %v", err)
		return res
	}

	logf("opening new tab")
	tabParent := ctx
	if leaveOpen {
		tabParent = parentCtx
	}
	tabCtx, tabCancel := newTab(tabParent)

	closeTab := func() {
		if !leaveOpen {
			tabCancel()
		}
	}

	logf("navigating to %s", url)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(readySelector, chromedp.ByQuery),
	); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("navigate: %v", err)
		return res
	}
	logf("page loaded — settling %s", opts.SettleDelay)
	time.Sleep(opts.SettleDelay)

	logf("extracting page info")
	var rawPage string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(pageInfoJS, &rawPage)); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("page eval: %v", err)
		return res
	}
	var pi pageInfoRaw
	if err := json.Unmarshal([]byte(rawPage), &pi); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("page unmarshal: %v", err)
		return res
	}
	res.Name = cleanText(pi.Name)
	res.ID = cleanText(pi.ID)
	res.Status = cleanText(pi.Status)
	logf("page info: name=%q id=%q", res.Name, res.ID)

	logf("clicking %q", triggerText)
	coords, err := findButton(tabCtx, triggerText)
	if err != nil {
		closeTab()
		res.Error = fmt.Sprintf("find %s: %v", triggerText, err)
		return res
	}
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("click %s: %v", triggerText, err)
		return res
	}

	logf("waiting for dialog")
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(dialogReadySelector, chromedp.ByQuery),
	); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("wait dialog: %v", err)
		return res
	}

	logf("clicking %q", optionText)
	optDeadline := time.Now().Add(opts.SettleDelay * 10)
	for time.Now().Before(optDeadline) {
		c, err := findButton(tabCtx, optionText)
		if err == nil {
			_ = dispatchClick(tabCtx, c.X, c.Y)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	logf("waiting for form ready")
	formWaitDeadline := time.Now().Add(opts.SettleDelay * 10)
	for time.Now().Before(formWaitDeadline) {
		var ready bool
		err := chromedp.Run(tabCtx, chromedp.Evaluate(formReadyJS, &ready))
		if err != nil {
			break
		}
		if ready {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	logf("selecting permission %q", opts.Permission)
	if err := selectPermission(tabCtx, opts.Permission); err != nil {
		logf("permission select: %v", err)
	}

	logf("filling justification")
	if err := fillJustification(tabCtx, opts.Justification); err != nil {
		logf("justification: %v", err)
	}

	if opts.AcceptTerms {
		logf("checking T&Cs (--accept-terms)")
		if err := checkTerms(tabCtx); err != nil {
			logf("terms: %v", err)
		}
	}

	if opts.DryRun == DryRunNone {
		submitTexts := viper.GetStringSlice("portal.dialog.submitTexts")
		if len(submitTexts) == 0 {
			submitTexts = []string{"Submit"}
		}
		var submitCoords Coords
		var submitErr error
		for _, text := range submitTexts {
			logf("looking for submit button %q", text)
			submitCoords, submitErr = findButton(tabCtx, text)
			if submitErr == nil {
				break
			}
		}
		if submitErr != nil {
			res.Error = fmt.Sprintf("submit button not found: %v", submitErr)
			tabCancel()
			return res
		}
		if err := dispatchClick(tabCtx, submitCoords.X, submitCoords.Y); err != nil {
			res.Error = fmt.Sprintf("submit click: %v", err)
			tabCancel()
			return res
		}
		logf("submitted — closing tab")
		time.Sleep(opts.SettleDelay)
		tabCancel()
	} else {
		logf("server dry-run: form filled, tab left open for review")
	}

	return res
}

// renewMembership navigates to the My Memberships page, selects a single
// entitlement by name, clicks Renew, fills the justification, and
// optionally submits. Each call opens its own tab so multiple renewals
// can run concurrently despite the portal's one-at-a-time constraint.
//
// In server dry-run mode, tabs are left open on both success and failure
// so the user can inspect the portal state.
func renewMembership(ctx context.Context, name string, opts renewOpts) Resource {
	res := Resource{Name: name}
	logf := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "  [verbose] "+format+"\n", args...)
		}
	}

	leaveOpen := opts.DryRun == DryRunServer

	// Keep a reference to the parent context before timeout wrapping.
	// Tabs created in DryRunServer mode must outlive the per-operation
	// timeout so they remain open for the user to inspect.
	parentCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	membershipsURL := viper.GetString("portal.memberships.url")
	if membershipsURL == "" {
		res.Error = "portal.memberships.url not configured"
		return res
	}
	tableReadySelector := viper.GetString("portal.memberships.tableReadySelector")
	if tableReadySelector == "" {
		tableReadySelector = "tbody[role='rowgroup'] tr[role='row']"
	}
	renewButtonText := viper.GetString("portal.memberships.renewButtonText")
	if renewButtonText == "" {
		renewButtonText = "Renew"
	}
	dialogReadySelector := viper.GetString("portal.memberships.dialogReadySelector")
	if dialogReadySelector == "" {
		dialogReadySelector = viper.GetString("portal.dialog.readySelector")
	}

	selectJS, err := resolveScript("portal.memberships.selectJs")
	if err != nil {
		res.Error = fmt.Sprintf("config: %v", err)
		return res
	}
	formReadyJS, err := resolveScript("portal.formReadyJs")
	if err != nil {
		res.Error = fmt.Sprintf("config: %v", err)
		return res
	}

	logf("opening new tab")
	tabParent := ctx
	if leaveOpen {
		tabParent = parentCtx
	}
	tabCtx, tabCancel := newTab(tabParent)

	closeTab := func() {
		if !leaveOpen {
			tabCancel()
		}
	}

	logf("navigating to %s", membershipsURL)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(membershipsURL),
		chromedp.WaitVisible(tableReadySelector, chromedp.ByQuery),
	); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("navigate memberships: %v", err)
		return res
	}
	logf("table loaded — settling %s", opts.SettleDelay)
	time.Sleep(opts.SettleDelay)

	logf("selecting checkbox for %q", name)
	js := fmt.Sprintf(selectJS, name)
	var selected bool
	selectDeadline := time.Now().Add(opts.SettleDelay * 5)
	for {
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, &selected)); err != nil {
			closeTab()
			res.Error = fmt.Sprintf("select checkbox: %v", err)
			return res
		}
		if selected {
			break
		}
		if time.Now().After(selectDeadline) {
			closeTab()
			res.Error = fmt.Sprintf("checkbox for %q not found in memberships table", name)
			return res
		}
		logf("checkbox not yet found, retrying…")
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)

	logf("clicking %q", renewButtonText)
	coords, err := findButton(tabCtx, renewButtonText)
	if err != nil {
		closeTab()
		res.Error = fmt.Sprintf("find %s: %v", renewButtonText, err)
		return res
	}
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("click %s: %v", renewButtonText, err)
		return res
	}

	logf("waiting for renew dialog")
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(dialogReadySelector, chromedp.ByQuery),
	); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("wait renew dialog: %v", err)
		return res
	}
	time.Sleep(opts.SettleDelay)

	logf("waiting for form elements to render")
	formWaitDeadline := time.Now().Add(opts.SettleDelay * 10)
	for time.Now().Before(formWaitDeadline) {
		var ready bool
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(formReadyJS, &ready)); err != nil {
			break
		}
		if ready {
			logf("form elements detected")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	logf("filling justification")
	if err := fillJustification(tabCtx, opts.Justification); err != nil {
		closeTab()
		res.Error = fmt.Sprintf("justification: %v", err)
		return res
	}
	time.Sleep(300 * time.Millisecond)

	if opts.AcceptTerms {
		logf("checking T&Cs (--accept-terms)")
		if err := checkTerms(tabCtx); err != nil {
			logf("terms: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}

	if opts.DryRun == DryRunNone {
		logf("clicking renew submit")
		submitCoords, err := findButton(tabCtx, renewButtonText)
		if err != nil {
			res.Error = fmt.Sprintf("renew submit button not found: %v", err)
			tabCancel()
			return res
		}
		if err := dispatchClick(tabCtx, submitCoords.X, submitCoords.Y); err != nil {
			res.Error = fmt.Sprintf("renew submit click: %v", err)
			tabCancel()
			return res
		}
		logf("renewed — closing tab")
		time.Sleep(opts.SettleDelay)
		tabCancel()
	} else {
		logf("server dry-run: justification filled, tab left open for review")
	}

	return res
}

// listMemberships navigates to the memberships page, waits for the
// table to render, evaluates the memberships-list JS script, and
// returns structured membership data.
func listMemberships(ctx context.Context, opts surveyOpts) ([]Membership, error) {
	logf := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "  [verbose] "+format+"\n", args...)
		}
	}

	membershipsURL := viper.GetString("portal.memberships.url")
	if membershipsURL == "" {
		return nil, fmt.Errorf("portal.memberships.url not configured")
	}
	tableReadySelector := viper.GetString("portal.memberships.tableReadySelector")
	if tableReadySelector == "" {
		tableReadySelector = "tbody[role='rowgroup'] tr[role='row']"
	}

	listJS, err := resolveScript("portal.memberships.listJs")
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	logf("opening new tab for memberships")
	tabCtx, tabCancel := newTab(ctx)
	defer tabCancel()

	logf("navigating to %s", membershipsURL)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(membershipsURL),
		chromedp.WaitVisible(tableReadySelector, chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("navigate memberships: %w", err)
	}
	logf("table loaded — settling %s", opts.SettleDelay)
	time.Sleep(opts.SettleDelay)

	logf("extracting membership data")
	var raw string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(listJS, &raw)); err != nil {
		return nil, fmt.Errorf("memberships eval: %w", err)
	}

	var memberships []Membership
	if err := json.Unmarshal([]byte(raw), &memberships); err != nil {
		return nil, fmt.Errorf("memberships unmarshal: %w", err)
	}
	for i := range memberships {
		memberships[i].Name = cleanText(memberships[i].Name)
		memberships[i].ID = cleanText(memberships[i].ID)
		memberships[i].SelfLink = strings.TrimSpace(memberships[i].SelfLink)
		memberships[i].Account = cleanText(memberships[i].Account)
		memberships[i].Role = cleanText(memberships[i].Role)
		memberships[i].ExpirationDate = cleanText(memberships[i].ExpirationDate)
	}
	logf("found %d memberships", len(memberships))
	return memberships, nil
}

// surveyResource navigates to a single resource URL, extracts all page
// and form information, and returns a populated Resource struct. All
// page interaction details (selectors, JS, button text) come from the
// portal.* config via Viper.
func surveyResource(ctx context.Context, url string, kind string, opts surveyOpts) Resource {
	ent := Resource{Kind: kind, SelfLink: url}
	logf := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "  [verbose] "+format+"\n", args...)
		}
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	readySelector := viper.GetString("portal.page.readySelector")
	triggerText := viper.GetString("portal.dialog.triggerText")
	dialogReadySelector := viper.GetString("portal.dialog.readySelector")
	optionText := viper.GetString("portal.dialog.optionText")

	pageInfoJS, err := resolveScript("portal.page.infoJs")
	if err != nil {
		ent.Error = fmt.Sprintf("config: %v", err)
		return ent
	}
	formReadyJS, err := resolveScript("portal.formReadyJs")
	if err != nil {
		ent.Error = fmt.Sprintf("config: %v", err)
		return ent
	}
	formInfoJS, err := resolveScript("portal.form.infoJs")
	if err != nil {
		ent.Error = fmt.Sprintf("config: %v", err)
		return ent
	}

	logf("opening new tab")
	tabCtx, tabCancel := newTab(ctx)
	defer tabCancel()

	logf("navigating to %s", url)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(readySelector, chromedp.ByQuery),
	); err != nil {
		ent.Error = fmt.Sprintf("navigate: %v", err)
		return ent
	}
	logf("page loaded, %q visible — settling %s", readySelector, opts.SettleDelay)
	time.Sleep(opts.SettleDelay)

	logf("extracting page info via JS")
	var rawPage string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(pageInfoJS, &rawPage)); err != nil {
		ent.Error = fmt.Sprintf("page eval: %v", err)
		return ent
	}
	var pi pageInfoRaw
	if err := json.Unmarshal([]byte(rawPage), &pi); err != nil {
		ent.Error = fmt.Sprintf("page unmarshal: %v", err)
		return ent
	}
	ent.Name = cleanText(pi.Name)
	ent.ID = cleanText(pi.ID)
	ent.Status = cleanText(pi.Status)
	ent.Domains = cleanTexts(pi.Domains)
	ent.Description = cleanText(pi.Description)
	ent.PrimaryOwners = cleanTexts(pi.PrimaryOwners)
	ent.SecondaryOwners = cleanTexts(pi.SecondaryOwners)
	if pi.CustomJustification != nil {
		c := cleanText(*pi.CustomJustification)
		ent.CustomJustification = &c
	}
	if pi.TermsAndConditions != nil {
		c := cleanText(*pi.TermsAndConditions)
		ent.TermsAndConditions = &c
	}
	logf("page info: name=%q id=%q status=%q", ent.Name, ent.ID, ent.Status)

	logf("looking for %q element", triggerText)
	coords, err := findButton(tabCtx, triggerText)
	if err != nil {
		ent.Error = fmt.Sprintf("find %s: %v", triggerText, err)
		return ent
	}
	logf("found <%s role=%q> at (%v,%v) size %dx%d — clicking",
		coords.Tag, coords.Role, coords.X, coords.Y, coords.W, coords.H)
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		ent.Error = fmt.Sprintf("click %s: %v", triggerText, err)
		return ent
	}

	logf("waiting for dialog %q", dialogReadySelector)
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(dialogReadySelector, chromedp.ByQuery),
	); err != nil {
		ent.Error = fmt.Sprintf("wait dialog: %v", err)
		return ent
	}
	logf("dialog visible")

	logf("waiting for %q button to render", optionText)
	var optionCoords Coords
	optDeadline := time.Now().Add(opts.SettleDelay * 10)
	for time.Now().Before(optDeadline) {
		c, err := findButton(tabCtx, optionText)
		if err == nil {
			optionCoords = c
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if optionCoords.W > 0 {
		logf("found %q <%s> at (%v,%v) size %dx%d — clicking",
			optionText, optionCoords.Tag, optionCoords.X, optionCoords.Y,
			optionCoords.W, optionCoords.H)
		if err := dispatchClick(tabCtx, optionCoords.X, optionCoords.Y); err != nil {
			logf("%q click error: %v", optionText, err)
		}
	} else {
		logf("%q not found after polling — skipping", optionText)
	}

	// Poll for form readiness rather than blind-sleeping. The dialog's
	// form content loads asynchronously; we wait until form elements
	// appear inside the dialog.
	logf("waiting for form elements to render (timeout %s)", opts.SettleDelay*10)
	formReady := false
	formWaitDeadline := time.Now().Add(opts.SettleDelay * 10)
	for time.Now().Before(formWaitDeadline) {
		var ready bool
		err := chromedp.Run(tabCtx, chromedp.Evaluate(formReadyJS, &ready))
		if err != nil {
			logf("form ready check error: %v", err)
			break
		}
		if ready {
			logf("form elements detected")
			formReady = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !formReady {
		logf("form elements not detected after polling, proceeding with settle delay %s", opts.SettleDelay)
		time.Sleep(opts.SettleDelay)
	}

	logf("extracting form info via JS")
	var rawForm string
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(formInfoJS, &rawForm)); err != nil {
		ent.Error = fmt.Sprintf("form eval: %v", err)
		return ent
	}
	var fi formInfoRaw
	if err := json.Unmarshal([]byte(rawForm), &fi); err != nil {
		ent.Error = fmt.Sprintf("form unmarshal: %v", err)
		return ent
	}
	for i := range fi.Roles {
		fi.Roles[i].Name = cleanText(fi.Roles[i].Name)
	}
	form := &RequestForm{
		Account:          cleanText(fi.Account),
		AccountOptions:   cleanTexts(fi.AccountOptions),
		Permissions:      fi.Roles,
		HasTermsCheckbox: fi.HasTermsCheckbox,
		HasJustification: fi.HasJustification,
	}
	if fi.TermsCheckboxLabel != nil {
		form.TermsCheckboxLabel = cleanText(*fi.TermsCheckboxLabel)
	}
	if fi.TermsText != nil {
		form.TermsText = cleanText(*fi.TermsText)
	}
	ent.RequestForm = form
	logf("form: account=%q roles=%d terms_checkbox=%v justification_field=%v",
		form.Account, len(fi.Roles), fi.HasTermsCheckbox, fi.HasJustification)

	logf("closing dialog")
	if coords, err := findCloseButton(tabCtx); err == nil {
		_ = dispatchClick(tabCtx, coords.X, coords.Y)
	}

	logf("done")
	return ent
}
