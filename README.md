# authzer

Declarative access management, even if the only API is a button.

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

# Import a site-pack as a named context
authzer config import -f site-pack.yaml
authzer config import -f site-pack.yaml --values values.yaml

# Import from a git repository (auth via credential helper)
authzer config import -f https://github.com/ORG/REPO/path/site-pack.yaml
authzer config import -f https://github.com/ORG/REPO/path/site-pack.yaml?ref=v1
authzer config import -f https://github.com/ORG/REPO/path/site-pack.yaml \
  --values https://github.com/ORG/REPO/path/values.yaml

# Context management
authzer config list
authzer config current
authzer config use staging
authzer config view
authzer config policy

# Trust management for remote imports
authzer config trust add example.com
authzer config trust add-identity user@example.com --issuer https://github.com/login/oauth
authzer config trust add-key https://github.com/ORG/REPO/signing-key.pub
authzer config trust list

# Override context for a single command
authzer --context staging get
```

### Getting started

```bash
# Import a site-pack (creates a named context)
authzer config import -f site-pack.yaml

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

### Contexts

`authzer` supports multiple portal configurations through named contexts.
Each context is a self-contained directory with its own `config.yaml`,
`policy.yaml`, scripts, and cache.

```bash
# Import site-packs for different portals
authzer config import -f portal-a.yaml
authzer config import -f portal-b.yaml

# List contexts
authzer config list
# CURRENT   NAME        PATH
# *         portal-a    portal-a
#           portal-b    portal-b

# Switch context
authzer config use portal-b
```

When no contexts are registered, `authzer` falls back to loading
`config.yaml` directly from `~/.config/authzer/` or the current
directory (flat mode).

### Config file

When using contexts, config is loaded from the active context directory
(e.g. `~/.config/authzer/CONTEXT_NAME/config.yaml`). Without contexts,
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

A site-pack is a self-contained YAML manifest (`kind: SitePack`) that bundles
all site-specific configuration into a single file. Templates are rendered
with user-supplied values; data files (scripts) are written verbatim.

```yaml
apiVersion: authzer/v1alpha1
kind: SitePack
metadata:
  name: my-portal
  annotations:
    description: "Site pack for my entitlement portal"
values:
  - key: group
    prompt: "RBAC group name"
    default: "sre"
  - key: portal_host
    prompt: "Portal base URL"
    default: "https://portal.example.com"
templates:
  config.yaml: |
    apiVersion: authzer/v1alpha1
    kind: Config
    group: {{ .group }}
    # ...
  policy.yaml: |
    ---
    apiVersion: authzer/v1alpha1
    kind: Role
    # ...
data:
  scripts/page-info.js: |
    (() => { /* extract page metadata */ })()
  scripts/form-info.js: |
    (() => { /* extract form fields */ })()
```

Import a site-pack with:

```bash
# Interactive — prompts for each value, creates a named context
authzer config import -f site-pack.yaml

# Non-interactive — supply values from a file
authzer config import -f site-pack.yaml --values values.yaml

# Explicit context name
authzer config import -f site-pack.yaml --context staging
```

This renders templates, copies scripts, and writes everything to a
context directory under `~/.config/authzer/`. The context is registered
and set as current. Values are saved alongside the config for future
re-imports.

JavaScript files are referenced from the rendered config using the `@` prefix:

```yaml
portal:
  page:
    infoJs: "@scripts/page-info.js"
```

At runtime, `authzer` resolves `@`-prefixed values to file paths relative to
the config file location.

### Remote URL handling

`authzer config import -f` and `authzer config trust add-key` accept HTTPS
URLs. Two URL styles are supported:

**Git repository file URLs** (recommended for private repos). For known
forge hosts (GitHub, GitLab, Bitbucket, Codeberg, sr.ht), the repository
boundary is inferred from the URL structure. Files are fetched via shallow
`git clone`, with auth delegated to the configured credential helper
(GCM, `gh auth setup-git`, netrc, keychain, etc.):

```bash
authzer config import -f https://github.com/ORG/REPO/path/site-pack.yaml
```

Append `?ref=TAG` to pin to a tag or branch (defaults to HEAD):

```bash
authzer config import -f https://github.com/ORG/REPO/path/site-pack.yaml?ref=v1
```

For self-hosted forges, use the `//` separator or include `.git` in the
URL to mark the repository boundary:

```bash
authzer config import -f https://git.example.com/group/repo//path/site-pack.yaml
authzer config import -f https://git.example.com/group/repo.git/path/site-pack.yaml
```

The `--values` flag also accepts URLs, using the same resolution rules:

```bash
authzer config import \
  -f https://github.com/ORG/REPO/path/site-pack.yaml?ref=v1 \
  --values https://github.com/ORG/REPO/path/values.yaml?ref=v1
```

**Plain HTTPS URLs** (public URLs only). Fetched via HTTP GET with no
authentication.

Release asset URLs (e.g. `/releases/download/...`) are not supported.
Site-pack files should live in the repository tree.

### Trust and verification

Remote site-pack imports are verified against a configurable trust chain.
Three verification methods are supported, tried in priority order:

| Method | Trust anchor | Sidecar file | Tool required |
|--------|-------------|--------------|---------------|
| Sigstore | OIDC identity (e.g. email) | `.sigstore.json` | `cosign` |
| SSH | SSH public key | `.sig` | `ssh-keygen` |
| Domain | Hostname allowlist | none | none |

When importing from a URL, `authzer` checks for a sigstore bundle or SSH
signature alongside the manifest. If found, the signature must verify
against a trusted identity or key. If no signature exists, the source
domain must be in the trusted list.

To skip verification for a one-off import (not recommended):

```bash
authzer config import -f https://untrusted.example.com/pack.yaml \
  --insecure-skip-source-verify
```

Publishers sign site-packs using standard tooling:

```bash
# Sigstore (keyless, authenticates via OIDC):
cosign sign-blob --bundle site-pack.yaml.sigstore.json site-pack.yaml

# SSH (sign with an existing key):
ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n authzer site-pack.yaml
```

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
