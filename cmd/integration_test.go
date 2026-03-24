//go:build integration

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
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/spf13/viper"
)

var (
	mockServerURL string
	cdpWSURL      string
	chromeCmd     *exec.Cmd
)

func TestMain(m *testing.M) {
	// Start HTTP server for mock portal.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start listener: %v\n", err)
		os.Exit(1)
	}
	mockServerURL = fmt.Sprintf("http://%s", listener.Addr().String())

	mockDir := filepath.Join("testdata", "mock-portal")
	server := &http.Server{Handler: http.FileServer(http.Dir(mockDir))}
	go func() { _ = server.Serve(listener) }()

	// Find Chrome.
	chromePath := findChrome()
	if chromePath == "" {
		fmt.Fprintln(os.Stderr, "Chrome/Chromium not found, skipping integration tests")
		_ = server.Close()
		os.Exit(0)
	}

	// Start Chrome with remote debugging.
	cdpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get port for CDP: %v\n", err)
		os.Exit(1)
	}
	cdpPort := cdpListener.Addr().(*net.TCPAddr).Port
	_ = cdpListener.Close()

	userDataDir, err := os.MkdirTemp("", "authzer-test-chrome-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	chromeCmd = exec.Command(chromePath,
		fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--no-first-run",
		"--disable-extensions",
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		"about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start Chrome: %v\n", err)
		os.Exit(1)
	}

	cdpWSURL = fmt.Sprintf("ws://127.0.0.1:%d", cdpPort)

	// Wait for CDP to be ready.
	cdpHTTPURL := fmt.Sprintf("http://127.0.0.1:%d/json/version", cdpPort)
	ready := false
	for i := 0; i < 30; i++ {
		resp, err := http.Get(cdpHTTPURL)
		if err == nil {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		fmt.Fprintln(os.Stderr, "Chrome CDP not ready after 6s")
		_ = chromeCmd.Process.Kill()
		os.Exit(1)
	}

	code := m.Run()

	// Cleanup.
	_ = server.Close()
	if chromeCmd.Process != nil {
		_ = chromeCmd.Process.Kill()
		_ = chromeCmd.Wait()
	}
	_ = os.RemoveAll(userDataDir)

	os.Exit(code)
}

func findChrome() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		candidates = []string{"chromium-browser", "chromium", "google-chrome", "google-chrome-stable"}
	default:
		candidates = []string{"chrome", "chromium"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

func setupViperForIntegration(t *testing.T) {
	t.Helper()
	viper.Reset()

	viper.Set("portal.page.readySelector", "h1")
	viper.Set("portal.dialog.triggerText", "Request Membership")
	viper.Set("portal.dialog.triggerSelector", `button, a, [role="button"], [role="link"]`)
	viper.Set("portal.dialog.readySelector", `[role="dialog"]`)
	viper.Set("portal.dialog.optionText", "This Account")
	viper.Set("portal.dialog.closeTexts", []string{"Cancel", "Close"})
	viper.Set("portal.dialog.closeAriaLabels", []string{"Close"})
	viper.Set("portal.dialog.submitTexts", []string{"Submit", "Renew"})
	viper.Set("portal.form.roleExcludePatterns", []string{})

	scriptDir := filepath.Join("testdata", "scripts")
	scriptNames := []struct {
		file string
		key  string
	}{
		{"page-info.js", "portal.page.infoJs"},
		{"form-info.js", "portal.form.infoJs"},
		{"find-button.js", "portal.findButtonJs"},
		{"form-ready.js", "portal.formReadyJs"},
		{"find-close.js", "portal.findCloseJs"},
		{"select-permission.js", "portal.form.selectPermissionJs"},
		{"fill-justification.js", "portal.form.fillJustificationJs"},
		{"check-terms.js", "portal.form.checkTermsJs"},
		{"memberships-select.js", "portal.memberships.selectJs"},
		{"memberships-list.js", "portal.memberships.listJs"},
	}
	for _, s := range scriptNames {
		data, err := os.ReadFile(filepath.Join(scriptDir, s.file))
		if err != nil {
			t.Fatalf("reading %s: %v", s.file, err)
		}
		viper.Set(s.key, strings.TrimSpace(string(data)))
	}

	viper.Set("portal.form.readySelectors", []string{
		`[role="combobox"]`,
		`input[type="radio"]`,
		`[role="radio"]`,
		`textarea`,
		`[role="checkbox"]`,
		`input[type="checkbox"]`,
	})
}

func TestSurveyResource_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, true)
	defer cancel()

	opts := surveyOpts{
		SettleDelay: 500 * time.Millisecond,
		Timeout:     30 * time.Second,
		Verbose:     true,
	}

	url := mockServerURL + "/index.html"
	res := surveyResource(browserCtx, url, "Entitlement", opts)

	if res.Error != "" {
		t.Fatalf("surveyResource error: %s", res.Error)
	}
	if res.Kind != "Entitlement" {
		t.Errorf("Kind = %q, want %q", res.Kind, "Entitlement")
	}
	if res.Name != "Mock Test Resource" {
		t.Errorf("Name = %q, want %q", res.Name, "Mock Test Resource")
	}
	if res.ID != "mock-res-001" {
		t.Errorf("ID = %q, want %q", res.ID, "mock-res-001")
	}
	if res.Status != "Active" {
		t.Errorf("Status = %q, want %q", res.Status, "Active")
	}
	if res.SelfLink != url {
		t.Errorf("SelfLink = %q, want %q", res.SelfLink, url)
	}

	if res.RequestForm == nil {
		t.Fatal("RequestForm is nil")
	}
	f := res.RequestForm

	if f.Account != "testuser@example.com" {
		t.Errorf("Account = %q, want %q", f.Account, "testuser@example.com")
	}
	if len(f.Permissions) < 2 {
		t.Fatalf("expected at least 2 permissions, got %d", len(f.Permissions))
	}

	foundReadOnly := false
	for _, p := range f.Permissions {
		if p.Name == "ReadOnly" {
			foundReadOnly = true
			if !p.Selected {
				t.Error("ReadOnly should be selected")
			}
		}
	}
	if !foundReadOnly {
		t.Error("ReadOnly permission not found in form")
	}

	if !f.HasTermsCheckbox {
		t.Error("expected HasTermsCheckbox to be true")
	}
	if !f.HasJustification {
		t.Error("expected HasJustification to be true")
	}
}

func TestFindButton_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, false)
	defer cancel()

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	url := mockServerURL + "/index.html"

	// Use chromedp directly for navigation.
	if err := navigateAndWait(tabCtx, url); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	coords, err := findButton(tabCtx, "Request Membership")
	if err != nil {
		t.Fatalf("findButton: %v", err)
	}
	if coords.W == 0 || coords.H == 0 {
		t.Error("button has zero dimensions")
	}
	if coords.Tag != "button" {
		t.Errorf("Tag = %q, want %q", coords.Tag, "button")
	}
}

func TestDispatchClick_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, false)
	defer cancel()

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	url := mockServerURL + "/index.html"
	if err := navigateAndWait(tabCtx, url); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Scroll the button into view before measuring coordinates.
	if err := chromedp.Run(tabCtx,
		chromedp.ScrollIntoView("#requestBtn", chromedp.ByID),
	); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	coords, err := findButton(tabCtx, "Request Membership")
	if err != nil {
		t.Fatalf("findButton: %v", err)
	}
	t.Logf("button at (%v,%v) size %dx%d", coords.X, coords.Y, coords.W, coords.H)

	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		t.Fatalf("dispatchClick: %v", err)
	}

	// Wait for the dialog to appear.
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(`[role="dialog"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("dialog did not open after click: %v", err)
	}

	closeCoords, err := findButton(tabCtx, "Cancel")
	if err != nil {
		t.Fatalf("Cancel button not found after dialog opened: %v", err)
	}
	if closeCoords.W == 0 {
		t.Error("Cancel button has zero width")
	}
}

// navigateAndWait navigates to the URL and waits for h1 to appear.
func navigateAndWait(ctx context.Context, url string) error {
	return chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("h1", chromedp.ByQuery),
	)
}

// ---------------------------------------------------------------------------
// Renew CDP tests
// ---------------------------------------------------------------------------

func TestRenewResource_ServerDryRun(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, true)
	defer cancel()

	url := mockServerURL + "/index.html"
	opts := renewOpts{
		SettleDelay:   500 * time.Millisecond,
		Timeout:       30 * time.Second,
		Verbose:       true,
		Permission:    "ReadWrite",
		Justification: "Integration test justification",
		DryRun:        DryRunServer,
	}

	res := renewResource(browserCtx, url, "Entitlement", opts)

	if res.Error != "" {
		t.Fatalf("renewResource error: %s", res.Error)
	}
	if res.Name != "Mock Test Resource" {
		t.Errorf("Name = %q, want %q", res.Name, "Mock Test Resource")
	}
	if res.ID != "mock-res-001" {
		t.Errorf("ID = %q, want %q", res.ID, "mock-res-001")
	}
}

func TestRenewResource_FullExecution(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, true)
	defer cancel()

	url := mockServerURL + "/index.html"
	opts := renewOpts{
		SettleDelay:   500 * time.Millisecond,
		Timeout:       30 * time.Second,
		Verbose:       true,
		Permission:    "Admin",
		Justification: "Full execution test",
		DryRun:        DryRunNone,
	}

	res := renewResource(browserCtx, url, "Entitlement", opts)

	if res.Error != "" {
		t.Fatalf("renewResource error: %s", res.Error)
	}
	if res.Name != "Mock Test Resource" {
		t.Errorf("Name = %q, want %q", res.Name, "Mock Test Resource")
	}
}

func TestSelectPermission_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, false)
	defer cancel()

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	url := mockServerURL + "/index.html"
	if err := navigateAndWait(tabCtx, url); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	coords, err := findButton(tabCtx, "Request Membership")
	if err != nil {
		t.Fatalf("findButton: %v", err)
	}
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		t.Fatalf("dispatchClick trigger: %v", err)
	}
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(`[role="dialog"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("dialog: %v", err)
	}

	// Click "This Account".
	time.Sleep(500 * time.Millisecond)
	forMe, err := findButton(tabCtx, "This Account")
	if err != nil {
		t.Fatalf("findButton ForMyself: %v", err)
	}
	if err := dispatchClick(tabCtx, forMe.X, forMe.Y); err != nil {
		t.Fatalf("dispatchClick ForMyself: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := selectPermission(tabCtx, "ReadWrite"); err != nil {
		t.Fatalf("selectPermission: %v", err)
	}

	// Verify the radio is checked.
	var checked string
	js := `(() => {
		for (const r of document.querySelectorAll('[role="radio"]')) {
			if (r.textContent.trim() === 'ReadWrite')
				return r.getAttribute('aria-checked');
		}
		return 'not-found';
	})()`
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, &checked)); err != nil {
		t.Fatalf("check radio state: %v", err)
	}
	if checked != "true" {
		t.Errorf("ReadWrite aria-checked = %q, want %q", checked, "true")
	}
}

func TestFillJustification_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, false)
	defer cancel()

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	url := mockServerURL + "/index.html"
	if err := navigateAndWait(tabCtx, url); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	coords, err := findButton(tabCtx, "Request Membership")
	if err != nil {
		t.Fatalf("findButton: %v", err)
	}
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		t.Fatalf("dispatchClick trigger: %v", err)
	}
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(`[role="dialog"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("dialog: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	forMe, err := findButton(tabCtx, "This Account")
	if err != nil {
		t.Fatalf("findButton ForMyself: %v", err)
	}
	if err := dispatchClick(tabCtx, forMe.X, forMe.Y); err != nil {
		t.Fatalf("dispatchClick ForMyself: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	want := "SRE access renewal for operational duties"
	if err := fillJustification(tabCtx, want); err != nil {
		t.Fatalf("fillJustification: %v", err)
	}

	var got string
	js := `document.querySelector('textarea').value`
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, &got)); err != nil {
		t.Fatalf("read textarea: %v", err)
	}
	if got != want {
		t.Errorf("textarea value = %q, want %q", got, want)
	}
}

func TestCheckTerms_MockPortal(t *testing.T) {
	setupViperForIntegration(t)

	ctx := context.Background()
	browserCtx, cancel := connectBrowser(ctx, cdpWSURL, false)
	defer cancel()

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	url := mockServerURL + "/index.html"
	if err := navigateAndWait(tabCtx, url); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	coords, err := findButton(tabCtx, "Request Membership")
	if err != nil {
		t.Fatalf("findButton: %v", err)
	}
	if err := dispatchClick(tabCtx, coords.X, coords.Y); err != nil {
		t.Fatalf("dispatchClick trigger: %v", err)
	}
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(`[role="dialog"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("dialog: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	forMe, err := findButton(tabCtx, "This Account")
	if err != nil {
		t.Fatalf("findButton ForMyself: %v", err)
	}
	if err := dispatchClick(tabCtx, forMe.X, forMe.Y); err != nil {
		t.Fatalf("dispatchClick ForMyself: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if err := checkTerms(tabCtx); err != nil {
		t.Fatalf("checkTerms: %v", err)
	}

	var checked string
	js := `document.querySelector('[role="checkbox"]').getAttribute('aria-checked')`
	if err := chromedp.Run(tabCtx, chromedp.Evaluate(js, &checked)); err != nil {
		t.Fatalf("read checkbox state: %v", err)
	}
	if checked != "true" {
		t.Errorf("terms checkbox aria-checked = %q, want %q", checked, "true")
	}
}
