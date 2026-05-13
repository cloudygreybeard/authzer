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
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// extractCookies connects to the browser via CDP and extracts session
// cookies for the given target URLs. It navigates to loginURL
// (typically the portal UI) to trigger SSO, then uses
// network.GetCookies with the target URLs to collect all cookies the
// browser would send to those endpoints.
//
// loginURL should be a page that triggers authentication (e.g. the
// portal UI), not an API endpoint that may reject unauthenticated
// requests. targetURLs are the API endpoints whose cookies we need.
func extractCookies(ctx context.Context, browserCtx context.Context, loginURL string, targetURLs ...string) (*http.Client, error) {
	if _, err := url.Parse(loginURL); err != nil {
		return nil, fmt.Errorf("parsing login URL: %w", err)
	}

	tabCtx, tabCancel := newTab(browserCtx)
	defer tabCancel()

	logV(3, "cookies: navigating to %s to ensure auth", loginURL)
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(loginURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("navigating to login URL %s: %w", loginURL, err)
	}
	time.Sleep(1 * time.Second)

	collectURLs := append([]string{loginURL}, targetURLs...)
	logV(3, "cookies: collecting cookies for %d URLs", len(collectURLs))

	var cookies []*network.Cookie
	if err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs(collectURLs).Do(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("extracting cookies: %w", err)
	}

	logV(3, "cookies: extracted %d cookies for target URLs", len(cookies))

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no cookies found for target URLs; authenticate in the browser first")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}

	domainCookies := make(map[string][]*http.Cookie)
	for _, c := range cookies {
		hc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		scheme := "http"
		if c.Secure {
			scheme = "https"
		}
		host := c.Domain
		if strings.HasPrefix(host, ".") {
			host = host[1:]
		}
		key := fmt.Sprintf("%s://%s", scheme, host)
		domainCookies[key] = append(domainCookies[key], hc)
	}

	total := 0
	for origin, hcs := range domainCookies {
		u, err := url.Parse(origin)
		if err != nil {
			continue
		}
		jar.SetCookies(u, hcs)
		total += len(hcs)
		logV(4, "cookies: loaded %d cookies for %s", len(hcs), origin)
	}
	logV(3, "cookies: loaded %d cookies across %d domains", total, len(domainCookies))

	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}, nil
}
