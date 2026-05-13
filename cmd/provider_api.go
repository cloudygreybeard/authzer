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
	"io"
	"net/http"
	"strings"
	"sync"
	"text/template"

	"github.com/chromedp/chromedp"
)

// APIProvider implements the Provider interface using config-driven
// API calls. When a browser context is provided, API calls are
// executed as XHR from within a browser tab via CDP, inheriting the
// browser's session cookies (required for portals with HttpOnly or
// SameSite cookies). When an HTTP client is provided instead (e.g.
// in tests), standard Go HTTP requests are used.
//
// All portal-specific details (URLs, request shapes, field names)
// come from the APIBackend document delivered by a site-pack.
type APIProvider struct {
	backend    *APIBackend
	browserCtx context.Context
	tabCtx     context.Context
	httpClient *http.Client
	kinds      []string

	cacheMu sync.Mutex
	cached  []Assignment
}

// NewAPIProvider creates an APIProvider bound to a browser context.
// API calls are executed as in-browser XHR to inherit session cookies.
func NewAPIProvider(backend *APIBackend, browserCtx context.Context, kinds []string) *APIProvider {
	if len(kinds) == 0 {
		kinds = []string{"Entitlement"}
	}
	return &APIProvider{
		backend:    backend,
		browserCtx: browserCtx,
		kinds:      kinds,
	}
}

// newAPIProviderHTTP creates an APIProvider that uses a Go HTTP client
// directly. This is used in tests and when cookie extraction is viable.
func newAPIProviderHTTP(backend *APIBackend, client *http.Client, kinds []string) *APIProvider {
	if len(kinds) == 0 {
		kinds = []string{"Entitlement"}
	}
	return &APIProvider{
		backend:    backend,
		httpClient: client,
		kinds:      kinds,
	}
}

// ensureTab opens a persistent tab on the API origin if one doesn't
// already exist. It navigates to the auth.cookieURL configured in the
// APIBackend (e.g. the Swagger UI page on the API domain) which
// triggers SSO and establishes session cookies for the API origin.
// Subsequent same-origin XHR calls from this tab inherit those
// cookies.
func (p *APIProvider) ensureTab() error {
	if p.tabCtx != nil {
		return nil
	}

	loginURL := p.backend.Spec.Auth.CookieURL
	if loginURL == "" {
		loginURL = p.backend.Spec.BaseURL + "/swagger/index.html"
	}

	tabCtx, _ := newTab(p.browserCtx)
	logV(3, "api: navigating tab to API login: %s", loginURL)

	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(loginURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("navigating to API login %s: %w", loginURL, err)
	}

	p.tabCtx = tabCtx
	return nil
}

func (p *APIProvider) Name() string                     { return "api" }
func (p *APIProvider) Kinds() []string                  { return p.kinds }
func (p *APIProvider) Capabilities() ProviderCapability { return CapList | CapCheck | CapApply }

// templateData holds the variables available to endpoint body templates.
type templateData struct {
	Identity      map[string]string
	Entitlement   string
	Permission    string
	Justification string
	GroupName     string
	GroupDomain   string
}

// renderBody executes a Go template string with the given data.
func renderBody(tmplStr string, data templateData) (string, error) {
	if tmplStr == "" {
		return "", nil
	}
	t, err := template.New("body").Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing body template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering body template: %w", err)
	}
	return buf.String(), nil
}

// doRequest dispatches an API call via CDP XHR (browser context) or
// Go HTTP client, depending on how the provider was constructed.
func (p *APIProvider) doRequest(ctx context.Context, ep APIEndpoint, body string) ([]byte, int, error) {
	logV(5, "api: %s %s%s", ep.Method, p.backend.Spec.BaseURL, ep.Path)
	if body != "" {
		logV(7, "api: body: %s", truncateForLog([]byte(body), 512))
	}

	var respBody []byte
	var status int
	var err error

	if p.browserCtx != nil {
		respBody, status, err = p.doRequestXHR(ep, body)
	} else {
		respBody, status, err = p.doRequestHTTP(ctx, ep, body)
	}

	if err != nil {
		return nil, 0, err
	}

	logV(5, "api: %s %s -> %d (%d bytes)", ep.Method, ep.Path, status, len(respBody))
	logV(7, "api: response: %s", truncateForLog(respBody, 1024))

	return respBody, status, nil
}

// doRequestXHR executes an API call by injecting a synchronous
// XMLHttpRequest into a browser tab on the API origin. Because the
// tab is same-origin, the XHR inherits session cookies automatically.
func (p *APIProvider) doRequestXHR(ep APIEndpoint, body string) ([]byte, int, error) {
	if err := p.ensureTab(); err != nil {
		return nil, 0, fmt.Errorf("ensuring API tab: %w", err)
	}

	script := buildXHRScript(ep.Path, ep.Method, body)

	var resultJSON string
	if err := chromedp.Run(p.tabCtx,
		chromedp.Evaluate(script, &resultJSON),
	); err != nil {
		return nil, 0, fmt.Errorf("XHR to %s: %w", ep.Path, err)
	}

	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, 0, fmt.Errorf("parsing XHR result for %s: %w", ep.Path, err)
	}
	if result.Error != "" {
		return nil, 0, fmt.Errorf("XHR error for %s: %s", ep.Path, result.Error)
	}

	return []byte(result.Body), result.Status, nil
}

// doRequestHTTP executes an API call using a standard Go HTTP client.
// Used in tests and when direct HTTP auth is viable.
func (p *APIProvider) doRequestHTTP(ctx context.Context, ep APIEndpoint, body string) ([]byte, int, error) {
	url := p.backend.Spec.BaseURL + ep.Path

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, ep.Method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("building request for %s: %w", ep.Path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request %s %s: %w", ep.Method, ep.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response from %s: %w", ep.Path, err)
	}

	return respBody, resp.StatusCode, nil
}

// buildXHRScript generates JavaScript that performs a synchronous XHR
// from within a browser tab on the API origin. The relative path
// ensures same-origin semantics, inheriting session cookies.
func buildXHRScript(path, method, body string) string {
	bodyJS := "null"
	if body != "" {
		b, _ := json.Marshal(body)
		bodyJS = string(b)
	}
	pathJS, _ := json.Marshal(path)
	methodJS, _ := json.Marshal(method)

	return fmt.Sprintf(`(() => {
  try {
    const xhr = new XMLHttpRequest();
    xhr.open(%s, %s, false);
    if (%s !== null) xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.send(%s);
    return JSON.stringify({status: xhr.status, body: xhr.responseText});
  } catch(e) {
    return JSON.stringify({status: 0, body: '', error: e.name + ': ' + e.message});
  }
})()`, methodJS, pathJS, bodyJS, bodyJS)
}

// List enumerates current assignments by calling the list endpoint
// and mapping the JSON response via the configured field map.
func (p *APIProvider) List(ctx context.Context) ([]Assignment, error) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	if p.cached != nil {
		return p.cached, nil
	}

	ep := p.backend.Spec.Endpoints.List
	data := templateData{Identity: p.backend.Spec.Identity}
	body, err := renderBody(ep.BodyTemplate, data)
	if err != nil {
		return nil, err
	}

	respBody, status, err := p.doRequest(ctx, ep, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("list endpoint returned %d: %s", status, truncateForLog(respBody, 256))
	}

	var rawItems []map[string]interface{}
	if err := json.Unmarshal(respBody, &rawItems); err != nil {
		return nil, fmt.Errorf("parsing list response: %w", err)
	}

	seen := make(map[string]bool)
	assignments := make([]Assignment, 0, len(rawItems))
	deduped := 0
	for _, raw := range rawItems {
		a := mapAssignment(raw, ep.FieldMap)

		if a.ID == "" && a.Name == "" {
			rawName, _ := raw["Name"].(string)
			if rawName != "" {
				a.ID = rawName
				a.Name = rawName
				a.Kind = "Group"
			} else {
				continue
			}
		}

		if a.Kind == "" {
			a.Kind = "Entitlement"
		}

		key := a.ID + "\x00" + a.Role
		if seen[key] {
			deduped++
			continue
		}
		seen[key] = true
		assignments = append(assignments, a)
	}

	if deduped > 0 {
		logV(3, "api: deduplicated %d entries by id+role", deduped)
	}

	p.cached = assignments
	logV(3, "api: listed %d assignments (%d entitlements, %d groups)",
		len(assignments),
		countKind(assignments, "Entitlement"),
		countKind(assignments, "Group"))
	return assignments, nil
}

func countKind(assignments []Assignment, kind string) int {
	n := 0
	for _, a := range assignments {
		if a.Kind == kind {
			n++
		}
	}
	return n
}

// mapAssignment extracts Assignment fields from a raw JSON object
// using the configured field map. Keys in fieldMap are Assignment
// field names; values are the JSON keys in the response object.
func mapAssignment(raw map[string]interface{}, fieldMap map[string]string) Assignment {
	get := func(authzerField string) string {
		jsonKey := fieldMap[authzerField]
		if jsonKey == "" {
			jsonKey = authzerField
		}
		v, ok := raw[jsonKey]
		if !ok {
			return ""
		}
		switch val := v.(type) {
		case string:
			return val
		case nil:
			return ""
		default:
			return fmt.Sprintf("%v", val)
		}
	}

	return Assignment{
		ID:             get("id"),
		Name:           get("name"),
		SelfLink:       get("selfLink"),
		Account:        get("account"),
		Role:           get("role"),
		ExpirationDate: get("expirationDate"),
		State:          get("state"),
	}
}

// Check verifies whether a rule is satisfied by listing assignments
// and looking for a matching resource.
func (p *APIProvider) Check(ctx context.Context, rule Rule) (*CheckResult, error) {
	assignments, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, a := range assignments {
		if strings.EqualFold(a.ID, rule.Resource) || strings.EqualFold(a.Name, rule.Resource) {
			return &CheckResult{
				Rule:       rule,
				Satisfied:  true,
				Assignment: &a,
				Message:    fmt.Sprintf("active membership: %s (%s)", a.Name, a.Role),
			}, nil
		}
	}

	return &CheckResult{
		Rule:    rule,
		Message: "no matching membership found via API",
	}, nil
}

// Apply reconciles a single rule by validating and then submitting a
// renewal or new request via the API.
func (p *APIProvider) Apply(ctx context.Context, rule Rule, justification string, dryRun bool) (*ApplyResult, error) {
	data := templateData{
		Identity:      p.backend.Spec.Identity,
		Entitlement:   rule.Resource,
		Permission:    rule.Permission,
		Justification: justification,
	}

	if ep := p.backend.Spec.Endpoints.Validate; ep.Path != "" {
		body, err := renderBody(ep.BodyTemplate, data)
		if err != nil {
			return nil, fmt.Errorf("rendering validate body: %w", err)
		}

		respBody, status, err := p.doRequest(ctx, ep, body)
		if err != nil {
			return nil, fmt.Errorf("validate request: %w", err)
		}

		logV(3, "api: validate %s -> %d", rule.Resource, status)

		if status < 200 || status >= 300 {
			return &ApplyResult{
				Rule:    rule,
				Action:  "failed",
				Message: fmt.Sprintf("validation failed (%d): %s", status, truncateForLog(respBody, 256)),
			}, nil
		}

		var valResp map[string]interface{}
		if err := json.Unmarshal(respBody, &valResp); err == nil {
			if invalids, ok := valResp["InvalidMembers"].([]interface{}); ok && len(invalids) > 0 {
				return &ApplyResult{
					Rule:    rule,
					Action:  "failed",
					Message: fmt.Sprintf("validation rejected: %d invalid members", len(invalids)),
				}, nil
			}
			if errs, ok := valResp["Errors"].([]interface{}); ok && len(errs) > 0 {
				return &ApplyResult{
					Rule:    rule,
					Action:  "failed",
					Message: fmt.Sprintf("validation errors: %v", errs),
				}, nil
			}
		}

		auditLog.Info("api.validate.ok", map[string]any{
			"resource":   rule.Resource,
			"permission": rule.Permission,
			"status":     status,
		})
	}

	if dryRun {
		return &ApplyResult{
			Rule:    rule,
			Action:  "validated",
			Message: "dry-run: validated but not submitted",
		}, nil
	}

	ep := p.backend.Spec.Endpoints.Submit
	body, err := renderBody(ep.BodyTemplate, data)
	if err != nil {
		return nil, fmt.Errorf("rendering submit body: %w", err)
	}

	respBody, status, err := p.doRequest(ctx, ep, body)
	if err != nil {
		return nil, fmt.Errorf("submit request: %w", err)
	}

	auditLog.Info("api.submit", map[string]any{
		"resource":   rule.Resource,
		"permission": rule.Permission,
		"status":     status,
		"dryRun":     dryRun,
	})

	if status < 200 || status >= 300 {
		return &ApplyResult{
			Rule:    rule,
			Action:  "failed",
			Message: fmt.Sprintf("submit failed (%d): %s", status, truncateForLog(respBody, 256)),
			Error:   fmt.Errorf("submit returned %d", status),
		}, nil
	}

	p.cacheMu.Lock()
	p.cached = nil
	p.cacheMu.Unlock()

	return &ApplyResult{
		Rule:    rule,
		Action:  "renewed",
		Message: fmt.Sprintf("submitted via API (%d)", status),
	}, nil
}

// InvalidateCache clears the cached assignment list so the next List
// call fetches fresh data.
func (p *APIProvider) InvalidateCache() {
	p.cacheMu.Lock()
	p.cached = nil
	p.cacheMu.Unlock()
}

