# Documentation

[LinkMyPhone](../README.md) · [Getting started](getting-started/installation.md) · [CLI](reference/cli.md)

## Start here

- [Project overview](../README.md) — goals, current support and known gaps.
- [Installation](getting-started/installation.md) — Linux requirements and build steps.
- [First run](getting-started/first-run.md) — sign in, select a phone and test sync.

## Use and operate

- [Clipboard behavior](user-guide/clipboard.md) — formats, limits, startup behavior and conflict handling.
- [CLI reference](reference/cli.md) — commands, flags and feature operations.
- [Configuration](reference/configuration.md) — state paths, feature settings and timeouts.
- [systemd service](operations/systemd.md) — install, configure, inspect and remove the user service.
- [Session recovery](operations/session-recovery.md) — reconnect and token renewal behavior.
- [Troubleshooting](operations/troubleshooting.md) — identify failures and collect a redacted report.
- [Privacy and local state](operations/privacy-and-state.md) — credentials, keys, clipboard data and retained state.

## Develop

- [Architecture overview](architecture/overview.md) — startup, message flow and component ownership.
- [Feature modules](architecture/modules.md) — module contracts, lifecycle and capabilities.
- [Control plane](architecture/control-plane.md) — socket API and feature reconciliation.
- [Session resilience](architecture/session-resilience.md) — session ownership and fault handling.
- [Feature directory](../features/README.md) — built-in modules and their boundaries.
- [Source map](developer/source-map.md) — packages and entry points.
- [Contributing](developer/contributing.md) — changes, review and pull requests.
- [Testing](developer/testing.md) — deterministic tests and opt-in integration checks.

## Protocol and research

- [Authentication](protocol/authentication.md) — sign-in, enrollment, trust and session validation.
- [Transport](protocol/transport.md) — Hub Relay, SignalR and PLATFORM message routing.
- [Clipboard protocol](protocol/clipboard.md) — clipboard messages, publication and correlation.
- [Research index](research/README.md) — evidence, findings and validation records.

## Reference

- [Glossary](reference/glossary.md) — project and protocol terms.
- [Research method](research/method.md) — evidence collection and its limits.
- [Findings](research/findings.md) — source-backed observations and corrections.
- [Development history](research/history.md) — dated implementation milestones.
- [Validation](research/validation.md) — live checks, environments and unresolved behavior.
