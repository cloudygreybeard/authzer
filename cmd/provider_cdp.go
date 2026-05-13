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
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// CDPProvider implements Provider by driving a live browser via CDP.
// It wraps the existing CDP functions in cdp.go (listMemberships,
// renewMembership, renewResource) behind the generic Provider
// interface.
type CDPProvider struct {
	browserCtx context.Context
	opts       surveyOpts

	cacheMu sync.Mutex
	cached  []Assignment
}

// NewCDPProvider creates a CDPProvider bound to an existing browser
// context. The surveyOpts control settle delays and timeouts for all
// CDP interactions.
func NewCDPProvider(browserCtx context.Context, opts surveyOpts) *CDPProvider {
	return &CDPProvider{
		browserCtx: browserCtx,
		opts:       opts,
	}
}

func (p *CDPProvider) Name() string                     { return "cdp" }
func (p *CDPProvider) Kinds() []string                  { return []string{"Entitlement"} }
func (p *CDPProvider) Capabilities() ProviderCapability { return CapList | CapCheck | CapApply }

// List scrapes the memberships table from the portal via CDP and
// returns structured assignment data. Results are cached for the
// lifetime of this provider instance.
func (p *CDPProvider) List(ctx context.Context) ([]Assignment, error) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	if p.cached != nil {
		return p.cached, nil
	}

	assignments, err := listMemberships(p.browserCtx, p.opts)
	if err != nil {
		return nil, err
	}

	renewWithinDays := viper.GetInt("renewWithinDays")
	threshold := time.Now().AddDate(0, 0, renewWithinDays)
	for i := range assignments {
		if isExpiringWithin(assignments[i].ExpirationDate, threshold) {
			assignments[i].Expiring = true
		}
	}

	p.cached = assignments
	return assignments, nil
}

// Check verifies whether a rule is satisfied by listing assignments
// and looking for a matching resource.
func (p *CDPProvider) Check(ctx context.Context, rule Rule) (*CheckResult, error) {
	assignments, err := p.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, a := range assignments {
		if strings.EqualFold(a.Name, rule.Resource) || strings.EqualFold(a.ID, rule.Resource) {
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
		Message: "no matching membership found",
	}, nil
}

// Apply reconciles a single rule by renewing an existing membership or
// requesting a new one via CDP browser automation.
func (p *CDPProvider) Apply(_ context.Context, rule Rule, justification string, dryRun bool) (*ApplyResult, error) {
	mode := DryRunNone
	if dryRun {
		mode = DryRunServer
	}

	rOpts := renewOpts{
		SettleDelay:   p.opts.SettleDelay,
		Timeout:       p.opts.Timeout,
		Permission:    rule.Permission,
		Justification: justification,
		DryRun:        mode,
		AcceptTerms:   viper.GetBool("acceptTerms"),
	}

	if rule.SelfLink != "" {
		kind := rule.Kind
		if kind == "" {
			kind = "Entitlement"
		}
		res := renewResource(p.browserCtx, rule.SelfLink, kind, rOpts)
		if res.Error != "" {
			return &ApplyResult{
				Rule:    rule,
				Action:  "failed",
				Message: res.Error,
				Error:   fmt.Errorf("%s", res.Error),
			}, nil
		}
		action := "renewed"
		if dryRun {
			action = "prepared"
		}
		return &ApplyResult{
			Rule:    rule,
			Action:  action,
			Message: fmt.Sprintf("%s via CDP", action),
		}, nil
	}

	return &ApplyResult{
		Rule:    rule,
		Action:  "failed",
		Message: "no selfLink for CDP renewal",
		Error:   fmt.Errorf("rule %q has no selfLink", rule.Resource),
	}, nil
}

// CacheOnlyProvider implements Provider using only locally cached
// assignment data. It supports List and Check but not Apply, making
// it suitable for the status command when no browser is connected.
type CacheOnlyProvider struct {
	cacheDir string
	kinds    []string
}

// NewCacheOnlyProvider creates a provider that reads from the local
// memberships cache.
func NewCacheOnlyProvider(cacheDir string, kinds []string) *CacheOnlyProvider {
	if len(kinds) == 0 {
		kinds = []string{"Entitlement"}
	}
	return &CacheOnlyProvider{cacheDir: cacheDir, kinds: kinds}
}

func (p *CacheOnlyProvider) Name() string                     { return "cache" }
func (p *CacheOnlyProvider) Kinds() []string                  { return p.kinds }
func (p *CacheOnlyProvider) Capabilities() ProviderCapability { return CapList | CapCheck }

func (p *CacheOnlyProvider) List(_ context.Context) ([]Assignment, error) {
	return readMembershipsCache(p.cacheDir)
}

func (p *CacheOnlyProvider) Check(ctx context.Context, rule Rule) (*CheckResult, error) {
	assignments, err := p.List(ctx)
	if err != nil {
		return &CheckResult{
			Rule:    rule,
			Message: fmt.Sprintf("cache unavailable: %v (run 'authzer get' first)", err),
		}, nil
	}

	details, _ := readCache(fmt.Sprintf("%s/details-cache.yaml", p.cacheDir))
	nameByID := buildNameLookup(details)

	name := nameByID[rule.Resource]
	if name == "" {
		name = rule.Resource
	}

	for _, a := range assignments {
		if strings.EqualFold(a.Name, name) || strings.EqualFold(a.ID, rule.Resource) {
			return &CheckResult{
				Rule:       rule,
				Satisfied:  true,
				Assignment: &a,
				Message:    "active membership found in cache",
			}, nil
		}
	}

	return &CheckResult{
		Rule:    rule,
		Message: "no membership found (run 'authzer get' to refresh)",
	}, nil
}

func (p *CacheOnlyProvider) Apply(_ context.Context, rule Rule, _ string, _ bool) (*ApplyResult, error) {
	return nil, fmt.Errorf("cache-only provider does not support apply; connect a browser first")
}
