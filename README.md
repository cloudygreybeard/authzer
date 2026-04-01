# authzer

Config-driven web access authz management via Chrome DevTools Protocol (CDP).

## Why

In some operational environments, access permissions are managed through web
portals that require manual, repetitive click-through workflows. When those
permissions are short-lived and the portal is the only management interface
available— perhaps because directory synchronisation, API-based provisioning,
or infrastructure-as-code are not options— maintaining a large set of
entitlements by hand becomes a genuine operational risk. Renewals are
forgotten, access lapses, and business-critical work stalls.

`authzer` exists for that specific scenario. It treats browser-based
entitlement management as a last-resort automation target: where better
integration points are available, they should be preferred. But where
click-ops is the only path, `authzer` brings repeatability, version-controlled
policy, and a safe-by-default execution model to the process.

## How it works

`authzer` connects to a running browser via CDP, queries the current
membership state from the portal, and reconciles it against a declarative
RBAC policy. Memberships that are expiring are renewed; memberships that
are missing are requested. Portal memberships not covered by any policy
rule are still tracked and renewed when expiring. All portal-specific
knowledge— CSS selectors, button labels, JavaScript extraction scripts,
URLs— is defined externally in a YAML config file. The Go codebase is
entirely portal-agnostic.

Access policy is defined using a declarative RBAC model, following
conventions common to modern cloud-native tooling:

- **Roles** contain rules specifying which resources to manage and at what
  permission level.
- **Groups** represent organisational identities with default justification
  text.
- **RoleBindings** attach Groups to Roles.

A user sets `--group sre` and the tool resolves the complete set of
resources, permissions, and justification text from the policy.

## Installation

### Homebrew (macOS/Linux)

```bash
brew install cloudygreybeard/tap/authzer
```

### Quick install (Windows PowerShell)

```powershell
irm https://github.com/cloudygreybeard/authzer/releases/latest/download/install.ps1 | iex
```

### Go install

```bash
go install github.com/cloudygreybeard/authzer@latest
```

### From source

```bash
git clone https://github.com/cloudygreybeard/authzer
cd authzer
make install
```

## Usage

```bash
# Show version
authzer version

# List current memberships (tabular)
authzer get

# Structured output with cached deep metadata
authzer get -o yaml
authzer get -o json

# More columns
authzer get -o wide

# Resource names only (for scripting)
authzer get -o name

# Force deep re-survey of individual resource pages
authzer get --refresh

# Detailed view of one or more resources
authzer describe
authzer describe my-service

# Reconcile: show plan without acting
authzer apply --dry-run=client

# Reconcile: prepare forms, leave tabs open for review (default)
authzer apply

# Reconcile a single resource
authzer apply foo-service

# Reconcile: accept T&Cs checkboxes (T&Cs text shown in CLI output)
authzer apply --accept-terms

# Reconcile: full execution (fill, submit, close)
authzer apply --dry-run=none

# Launch a browser with CDP remote debugging enabled
authzer launch

# Validate setup: config, policy, browser, CDP connectivity
authzer doctor
```

### Getting started

```bash
# Check setup
authzer doctor

# Start browser with remote debugging
authzer launch

# List memberships
authzer get
```

`authzer launch` starts the configured browser with `--remote-debugging-port`,
`--remote-debugging-address`, and `--user-data-dir` flags. The dedicated
profile directory (default `~/.config/authzer/browser-profile`) isolates
automation sessions from normal browsing and satisfies Chrome 136+ security
requirements.

Browser settings can be configured in `config.yaml`:

```yaml
browser:
  path: ""                  # auto-detect if empty
  port: 9222
  address: 127.0.0.1
  userDataDir: ""           # default: ~/.config/authzer/browser-profile
  extraArgs: []
```

### Group selection

The `--group` flag determines which RBAC role bindings are resolved. It
can be specified on the command line for a one-off override, or set
persistently so it does not need to be repeated:

| Method | Example | Use case |
|--------|---------|----------|
| Config file | `group: sre` in `config.yaml` | Set once, use everywhere |
| Environment variable | `export AUTHZER_GROUP=manager` | Per-session or per-shell override |
| CLI flag | `--group auditor` | One-off override |

These follow the standard precedence order: flag > env > config. If no
group is configured, `authzer` exits with an error indicating where to
set one.

### Dry-run modes

The `--dry-run` flag controls execution depth and defaults to `server`
(safe by default):

| Mode | Behaviour |
|------|-----------|
| `client` | Local only. Parse config, resolve policy, enumerate rules. No browser contact. |
| `server` | Connect to browser, navigate, fill forms, but stop before submitting. Tabs are left open for manual review. |
| `none` | Full execution. Fill forms, click submit, close dialogs and tabs. |

Like `--group`, the dry-run mode can be pinned in `config.yaml`:

```yaml
dry-run: server
```

The shipped default is `server`. Users who are confident in their
configuration and prefer fully automated renewals can set `dry-run: none`
in their config and override with `--dry-run=server` when they want to
review.

### Terms and conditions

Some portal resources require the user to accept terms and conditions via
a checkbox. By default, `authzer apply` leaves these unticked— the
reconciliation listing annotates affected resources with `[has T&Cs]`
and leaves the tab open for manual review.

To accept terms automatically, pass `--accept-terms`. The full T&Cs
text from cached metadata is printed to stderr so the user can see
exactly what they are agreeing to:

```bash
authzer apply --accept-terms
```

This follows the same explicit-consent pattern used by tools like Helm
(`helm repo add --allow-deprecated-repos`) and Terraform
(`terraform apply -auto-approve`).

### Config file

`authzer` reads config from (in order of precedence):

1. CLI flags and `AUTHZER_` environment variables
2. `~/.config/authzer/config.yaml`
3. `config.yaml` in the current working directory

The config file defines portal interaction details (selectors, scripts,
dialog structure) and references a separate `policy.yaml` file containing
the RBAC resources.

See the [Configuration Guide](docs/configuration.md) for details on
creating a site-specific configuration.

### RBAC policy

Policy is defined as multi-document YAML using typed resource manifests:

```yaml
apiVersion: authzer/v1alpha1
kind: Role
metadata:
  name: team-access
  labels:
    team: platform
rules:
  - kind: Entitlement
    resource: my-service
    selfLink: https://portal.example.com/manage/resource/my-service
    permission: ReadOnly
---
apiVersion: authzer/v1alpha1
kind: Group
metadata:
  name: sre
justification: "Platform SRE team member"
---
apiVersion: authzer/v1alpha1
kind: RoleBinding
metadata:
  name: sre-team-access
subjects:
  - kind: Group
    name: sre
roleRef:
  kind: Role
  name: team-access
```

Roles support aggregation via label selectors, allowing composed roles
that merge rules from multiple base roles.

## Architecture

### Two-layer separation

**Mechanism (this repository):** A generic browser automation engine with
RBAC policy resolution. No portal-specific code.

**Policy (separate, private config):** Site-specific configuration defining
portal selectors, JavaScript extraction scripts, resource URLs, and RBAC
policy. Distributed via site-packs.

### Site-packs

A site-pack is a portable bundle of site-specific configuration:

```
site-pack/
  site.yaml             # Pack metadata
  values.yaml           # Template variable values
  values.example.yaml   # Documented example values
  templates/            # Go text/template files
    config.yaml.tpl     # Portal interaction config
    policy.yaml.tpl     # RBAC policy with resource URLs
  scripts/              # JavaScript files for portal interaction
    page-info.js        # Extract page metadata
    form-info.js        # Extract form fields
    find-button.js      # Locate trigger buttons
    form-ready.js       # Poll for form readiness
    find-close.js       # Locate close/cancel buttons
    select-permission.js  # Click a permission radio button
    fill-justification.js # Populate justification textarea
    check-terms.js        # Accept terms checkbox
    memberships-list.js   # Scrape memberships table
    memberships-select.js # Select a membership checkbox
```

JavaScript files are referenced from config using the `@` prefix:

```yaml
portal:
  page:
    infoJs: "@scripts/page-info.js"
```

At runtime, `authzer` resolves `@`-prefixed values to file paths relative to
the config file location.

## Development

```bash
make build              # Build the binary
make test               # Run unit tests
make test-integration   # Run CDP integration tests (requires Chrome)
make test-all           # Run all tests
make lint               # Run golangci-lint
make snapshot           # Build a snapshot release
make clean              # Remove build artifacts
```

### Testing tiers

1. **Unit tests** (`make test`): Config parsing, RBAC resolution, output
   format stability (golden files). Always run in CI.
2. **Integration tests** (`make test-integration`): End-to-end CDP tests
   against a mock portal with headless Chrome. Build-tag gated
   (`//go:build integration`).
3. **Smoke tests** (`hack/`): Manual validation scripts for live portal
   testing on the target machine.

### Validating policy offline

```bash
authzer apply --dry-run=client --group sre
```

This resolves the RBAC chain, compares against cached membership state,
and prints the reconciliation plan without contacting a browser.

## License

Apache 2.0. See [LICENSE](LICENSE).
