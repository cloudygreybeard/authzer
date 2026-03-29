# Configuration Guide

This document describes how to create a site-specific configuration for
`authzer`. The configuration defines how `authzer` interacts with a
particular portal and which access entitlements to manage.

## Overview

`authzer` configuration consists of two files:

- **`config.yaml`**— Portal interaction settings: CDP endpoint, selectors,
  JavaScript extraction scripts, dialog structure, and general preferences.
- **`policy.yaml`**— RBAC policy defining resources, permissions, groups,
  and role bindings.

Both files are placed in `~/.config/authzer/` on the target machine.

## Config file locations

`authzer` searches for `config.yaml` in this order:

1. CLI flags and `AUTHZER_` environment variables (highest precedence)
2. `~/.config/authzer/config.yaml`
3. `config.yaml` in the current working directory

The `~/.config` path is used on all platforms, including Windows, where it
resolves to `%USERPROFILE%\.config`.

## config.yaml

### Minimal example

```yaml
apiVersion: authzer/v1alpha1
kind: Config

group: sre
concurrency: 3
settleDelay: 1s
verbose: false
policy: policy.yaml
dry-run: server
renewWithinDays: 30

survey:
  timeout: 120s

browser:
  path: ""                  # auto-detect if empty
  port: 9222
  address: 127.0.0.1
  userDataDir: ""           # default: ~/.config/authzer/browser-profile
  extraArgs: []
```

### Browser section

The `browser` keys configure how `authzer launch` starts a browser and
how `checkCDP` constructs launch hints. If `browser.path` is empty,
authzer auto-detects Edge, Chrome, or Chromium in well-known install
locations for the current platform.

| Key | Default | Description |
|-----|---------|-------------|
| `browser.path` | auto-detect | Absolute path to browser binary |
| `browser.port` | `9222` | `--remote-debugging-port` value |
| `browser.address` | `127.0.0.1` | `--remote-debugging-address` value |
| `browser.userDataDir` | `~/.config/authzer/browser-profile` | `--user-data-dir` value |
| `browser.extraArgs` | `[]` | Additional flags passed to the browser |

The `cdp` key (or `--cdp` flag) can still be used as an explicit override
for the CDP HTTP endpoint. If set, it takes precedence over the composed
`http://browser.address:browser.port` URL.

Chrome 136+ requires `--user-data-dir` to be set when using
`--remote-debugging-port`. Edge does not currently enforce this, but
using a dedicated profile is recommended regardless to isolate
automation sessions from normal browsing.

### Portal section

The `portal` section defines all site-specific interaction details. Every
selector, button label, and JavaScript extraction script is specified here.

```yaml
portal:
  name: "PORTAL_DISPLAY_NAME"

  page:
    readySelector: "READY_CSS_SELECTOR"
    infoJs: "@scripts/page-info.js"

  dialog:
    triggerText: "BUTTON_TEXT"
    triggerSelector: 'button, a, [role="button"]'
    readySelector: '[role="dialog"]'
    optionText: "DIALOG_OPTION_TEXT"
    closeTexts: ["Cancel", "Close"]
    closeAriaLabels: ["Close"]
    submitTexts: ["Submit", "Renew"]

  form:
    readySelectors:
      - '[role="combobox"]'
      - 'input[type="radio"]'
      - 'textarea'
    infoJs: "@scripts/form-info.js"
    selectPermissionJs: "@scripts/select-permission.js"
    fillJustificationJs: "@scripts/fill-justification.js"
    checkTermsJs: "@scripts/check-terms.js"
    roleExcludePatterns:
      - "PATTERN_TO_EXCLUDE"

  memberships:
    url: "MEMBERSHIPS_PAGE_URL"
    tableReadySelector: "tbody[role='rowgroup'] tr[role='row']"
    listJs: "@scripts/memberships-list.js"
    selectJs: "@scripts/memberships-select.js"
    renewButtonText: "Renew"
    dialogReadySelector: '[role="dialog"]'

  findButtonJs: "@scripts/find-button.js"
  formReadyJs: "@scripts/form-ready.js"
  findCloseJs: "@scripts/find-close.js"
```

Replace the placeholders:

- `PORTAL_DISPLAY_NAME` with the human-readable name of the portal.
- `READY_CSS_SELECTOR` with a CSS selector indicating the page has loaded.
- `BUTTON_TEXT` with the text of the button that opens the request dialog.
- `DIALOG_OPTION_TEXT` with the option to select in the initial dialog
  (e.g. "This Account").
- `PATTERN_TO_EXCLUDE` with text patterns to filter out from permission
  lists.
- `MEMBERSHIPS_PAGE_URL` with the URL of the portal's memberships listing
  page.

### JavaScript files

JavaScript extraction scripts are referenced using the `@` prefix, which
resolves to a file path relative to the config file:

```yaml
portal:
  page:
    infoJs: "@scripts/page-info.js"
```

This resolves to `~/.config/authzer/scripts/page-info.js` when the config
is in `~/.config/authzer/`.

Alternatively, inline JavaScript can be used directly:

```yaml
portal:
  page:
    infoJs: "(() => { return { title: document.title }; })()"
```

### Required JavaScript contracts

Each JavaScript file must return a specific data shape:

**page-info.js**— Returns page metadata:

```javascript
(() => {
  return {
    id: "resource-id",
    name: "Resource Name",
    status: "Active",
    domains: ["domain1.example.com"],
    description: "Resource description",
    primaryOwners: ["owner@example.com"],
    secondaryOwners: []
  };
})()
```

**form-info.js**— Returns form field details:

```javascript
(() => {
  return {
    account: "user@example.com",
    accountOptions: ["user@example.com"],
    roles: [
      { name: "ReadOnly", selected: false },
      { name: "ReadWrite", selected: true }
    ],
    hasTermsCheckbox: true,
    termsCheckboxLabel: "I accept",
    termsText: "Terms and conditions text...",
    hasJustificationField: true,
    customJustification: ""
  };
})()
```

**find-button.js**— Returns the bounding rect of the trigger button:

```javascript
(() => {
  // Locate the button and return its position for click dispatch
  const btn = /* locate button */;
  if (!btn) return null;
  const r = btn.getBoundingClientRect();
  return { x: r.x + r.width/2, y: r.y + r.height/2, width: r.width, height: r.height };
})()
```

**form-ready.js**— Returns `true` when the form is ready for interaction:

```javascript
(() => {
  // Check that expected form elements are present and visible
  return document.querySelector('textarea') !== null;
})()
```

**find-close.js**— Returns the bounding rect of the close/cancel button:

```javascript
(() => {
  const btn = /* locate close button */;
  if (!btn) return null;
  const r = btn.getBoundingClientRect();
  return { x: r.x + r.width/2, y: r.y + r.height/2, width: r.width, height: r.height };
})()
```

**select-permission.js**— Clicks the radio button matching the target
permission level. Receives the target text via `%s` placeholder (injected
by Go `fmt.Sprintf`). Returns `true` if found and clicked, `false` otherwise.

**fill-justification.js**— Focuses the justification textarea and inserts
text using `document.execCommand('insertText')`. Receives text via `%s`.
Returns `true` if text was successfully inserted.

**check-terms.js**— Finds and clicks the terms-and-conditions checkbox
if present and unchecked. Returns `true` if a checkbox was found.

**memberships-list.js**— Scrapes the memberships table and returns a JSON
array of objects with fields: `name`, `id`, `account`, `role`,
`expirationDate`, `expiring`.

**memberships-select.js**— Clicks the checkbox for a named membership row
in the memberships table. Receives the target name via `%s`. Returns `true`
if found and clicked.

## policy.yaml

The policy file defines RBAC resources as multi-document YAML.

### Role

A Role contains rules that map resources to permissions:

```yaml
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: ROLE_NAME
  labels:
    team: TEAM_LABEL
rules:
  - kind: RESOURCE_KIND
    resource: RESOURCE_ID
    selfLink: RESOURCE_URL
    permission: PERMISSION_LEVEL
```

Replace the placeholders:

- `ROLE_NAME` with a descriptive name for the role.
- `TEAM_LABEL` with a label value used for aggregation.
- `RESOURCE_KIND` with the site-specific resource type (e.g. `Entitlement`).
- `RESOURCE_ID` with the portal's identifier for the resource.
- `RESOURCE_URL` with the full URL to the resource management page.
- `PERMISSION_LEVEL` with the desired access level (must match an option
  presented by the portal's form).

### Group

A Group represents an organisational identity:

```yaml
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: GROUP_NAME
justification: "JUSTIFICATION_TEXT"
```

Replace `GROUP_NAME` with the group identifier used with `--group` and
`JUSTIFICATION_TEXT` with the default justification text submitted with
renewal requests.

### RoleBinding

A RoleBinding attaches a Group to a Role:

```yaml
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: BINDING_NAME
subjects:
  - kind: Group
    name: GROUP_NAME
roleRef:
  kind: Role
  name: ROLE_NAME
```

### Aggregated roles

Roles can aggregate rules from other roles using label selectors:

```yaml
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: all-access
aggregationRule:
  roleSelectors:
    - matchLabels:
        team: TEAM_LABEL
```

This merges all rules from roles carrying the matching label.

## Site-packs

For distributing configuration across machines, `authzer` supports a
site-pack format. A site-pack is a directory containing templates,
scripts, and values:

```
site-pack/
  site.yaml             # Pack metadata
  values.yaml           # Template variable values
  values.example.yaml   # Documented example values
  templates/
    config.yaml.tpl     # Config template
    policy.yaml.tpl     # Policy template
  scripts/              # JavaScript files
```

Templates use Go `text/template` syntax with values from `values.yaml`.
The rendered output is deployed to the target machine.

## Deployment

### Linux / macOS / WSL

```bash
mkdir -p ~/.config/authzer
cp config.yaml policy.yaml ~/.config/authzer/
cp -r scripts/ ~/.config/authzer/scripts/
```

### Windows (PowerShell)

```powershell
New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\.config\authzer"
Copy-Item config.yaml, policy.yaml "$env:USERPROFILE\.config\authzer\"
Copy-Item -Recurse scripts\ "$env:USERPROFILE\.config\authzer\scripts\"
```

## Browser setup

The simplest way to start a browser with CDP enabled is:

```bash
authzer launch
```

This auto-detects the browser, creates a dedicated profile directory,
and starts the process with the necessary flags. The profile directory
(default `~/.config/authzer/browser-profile`) isolates automation from
normal browsing and satisfies Chrome 136+ security requirements.

Alternatively, start the browser manually with the required flags:

```bash
# Linux
google-chrome --remote-debugging-port=9222 \
    --remote-debugging-address=127.0.0.1 \
    --user-data-dir=~/.config/authzer/browser-profile

# macOS
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    --remote-debugging-port=9222 \
    --remote-debugging-address=127.0.0.1 \
    --user-data-dir=~/.config/authzer/browser-profile

# Windows (PowerShell)
Start-Process "msedge" -ArgumentList @(
    "--remote-debugging-port=9222",
    "--remote-debugging-address=127.0.0.1",
    "--user-data-dir=$env:USERPROFILE\.config\authzer\browser-profile"
)
```

Verify the endpoint is accessible:

```bash
curl http://127.0.0.1:9222/json/version
```

## Validation

Validate your full setup:

```bash
# Comprehensive setup check
authzer doctor

# Check config parsing and policy resolution (no browser)
authzer apply --dry-run=client

# Check browser connectivity and scrape memberships (no form submission)
authzer get

# Detailed view of cached resources
authzer describe
```
