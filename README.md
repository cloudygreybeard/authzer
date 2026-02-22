# authzer

Config-driven CLI tool for managing web-based access entitlements
via Chrome DevTools Protocol (CDP).

## Why

In some operational environments, access permissions are managed through web
portals that require manual, repetitive click-through workflows. When those
permissions are short-lived and the portal is the only management interface
available -- perhaps because directory synchronisation, API-based provisioning,
or infrastructure-as-code are not options -- maintaining a large set of
entitlements by hand becomes a genuine operational risk. Renewals are
forgotten, access lapses, and business-critical work stalls.

`authzer` exists for that specific scenario. It treats browser-based
entitlement management as a last-resort automation target: where better
integration points are available, they should be preferred. But where
click-ops is the only path, `authzer` brings repeatability, version-controlled
policy, and a safe-by-default execution model to the process.

## Design goals

- **Config-driven.** All portal-specific knowledge (CSS selectors, button
  labels, JavaScript extraction scripts, URLs) lives in external YAML
  configuration. The Go codebase is entirely portal-agnostic.
- **Declarative policy.** Access entitlements are expressed as RBAC resources
  (Roles, Groups, RoleBindings) and reconciled against portal state.
- **Safe by default.** The default execution mode fills forms but stops
  before submitting, leaving tabs open for manual review.
- **Minimal dependencies.** Connects to an already-running browser instance
  via CDP rather than bundling a browser or requiring Selenium.

## Planned CLI surface

```
authzer get               # List current portal memberships
authzer get -o yaml       # Structured output with deep metadata
authzer describe          # Human-readable resource details
authzer apply             # Reconcile policy against portal state
authzer apply --dry-run   # Preview changes without acting
authzer version           # Print version
```

## Architecture

The tool separates mechanism from policy:

- **Mechanism (this repository):** A generic CDP automation engine with
  RBAC policy resolution. No portal-specific code.
- **Policy (separate, private config):** Site-specific configuration
  defining portal selectors, JavaScript hooks, resource URLs, and RBAC
  policy. Distributed as portable site-packs.

## Development

```bash
make build    # Build the binary
make test     # Run tests
make lint     # Run linter
make clean    # Remove build artifacts
```

## License

Apache 2.0. See [LICENSE](LICENSE).
