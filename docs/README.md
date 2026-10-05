# Documentation

[LinkMyPhone](../README.md) · [CLI reference](reference/cli.md) · [Troubleshooting](operations/troubleshooting.md)

LinkMyPhone is an independent, unofficial Phone Link client for Linux. It syncs clipboard text, HTML and images and shows phone notifications on the desktop.

These guides cover sign-in, clipboard transfers and notification actions and replies. See [current support](../README.md#current-support-and-broader-scope) for features that are not yet implemented.

## Start with your task

| You want to… | Start here | Continue with |
| --- | --- | --- |
| Understand the project goal and current support | [Project overview](../README.md) | [Architecture](architecture/overview.md) |
| Set up clipboard and notifications | [Installation](getting-started/installation.md) | [First run](getting-started/first-run.md) |
| Keep it running in the background | [systemd service](operations/systemd.md) | [Session recovery](operations/session-recovery.md) |
| Check clipboard formats | [Clipboard behavior](user-guide/clipboard.md) | [Privacy and local state](operations/privacy-and-state.md) |
| Set up notification actions and replies | [First run](getting-started/first-run.md#4-enable-clipboard-and-notifications-in-one-runtime) | [Notification settings](reference/configuration.md#notification-configuration) |
| Change the client | [Source map](developer/source-map.md) | [Contributing](developer/contributing.md) |
| Understand the protocol | [Authentication](protocol/authentication.md) | [Transport](protocol/transport.md) · [Clipboard messages](protocol/clipboard.md) |

For commands and options, see the [CLI reference](reference/cli.md) and [configuration](reference/configuration.md). The [glossary](reference/glossary.md) explains unfamiliar terms.

## Browse the library

<details>
<summary>Install, use, and operate</summary>

| Guide | What you will find |
| --- | --- |
| [Installation](getting-started/installation.md) | Linux requirements, clipboard providers, source access, and build steps. |
| [First run](getting-started/first-run.md) | Sign-in, enrollment, phone selection, and a two-way clipboard check. |
| [Clipboard behavior](user-guide/clipboard.md) | Supported formats, image limits, startup, clears, MIME polling, conflicts, and echo suppression. |
| [CLI](reference/cli.md) | Commands, flags, and live versus offline feature operations. |
| [Configuration](reference/configuration.md) | State paths, authentication records, feature settings, and timeouts. |
| [Session recovery](operations/session-recovery.md) | Reconnect, token renewal, interruption behavior and remaining live checks. |
| [systemd service](operations/systemd.md) | Install, readiness, session environment, custom profiles, logs, and uninstall. |
| [Troubleshooting](operations/troubleshooting.md) | Find the failing stage and collect a redacted report. |
| [Privacy and local state](operations/privacy-and-state.md) | Credentials, keys, permissions, clipboard exposure, backups, and retained state. |

</details>

<details>
<summary>Develop and test</summary>

| Guide | What you will find |
| --- | --- |
| [Architecture](architecture/overview.md) | Startup, message flow, ownership, and shutdown. |
| [Session resilience](architecture/session-resilience.md) | Supervisor ownership, supplied source observations and fault-test coverage. |
| [Feature modules](architecture/modules.md) | Manifests, lifecycle, dependencies, capabilities, and the built-in catalog. |
| [Control plane](architecture/control-plane.md) | Socket ownership, wire contracts, reconciliation, and offline state. |
| [Source map](developer/source-map.md) | Packages and entry points grouped by the behavior they own. |
| [Contributing](developer/contributing.md) | Branches, code changes, review evidence, and pull requests. |
| [Testing](developer/testing.md) | Deterministic checks and opt-in desktop, cloud, or service probes. |
| [Feature directory](../features/README.md) | Registration and the existing clipboard implementation. |
| [Glossary](reference/glossary.md) | Runtime and protocol terminology. |

</details>

<details>
<summary>Protocol and reverse engineering</summary>

| Reference or record | What you will find |
| --- | --- |
| [Research index](research/README.md) | Reading paths through wire references, findings, and validation. |
| [Authentication](protocol/authentication.md) | Microsoft tokens, DCG identity, enrollment, trust, discovery, wake, and validation. |
| [Transport](protocol/transport.md) | WebSocket, SignalR, Hub Relay, DCG packets, PLATFORM routing, and acknowledgments. |
| [Clipboard protocol](protocol/clipboard.md) | Fields, enums, routes, publication, correlation, and generation ordering. |
| [Research method](research/method.md) | Investigation steps, evidence collection, provenance, and absent artifacts. |
| [Findings](research/findings.md) | Observations, failed assumptions, fixes, and regression evidence. |
| [Development history](research/history.md) | Dated, commit-linked milestones. |
| [Validation](research/validation.md) | Historical and current live results, Windows image comparisons, test boundaries, and unresolved behavior. |

</details>

## Record a change

Update the affected guide and its technical explanation when behavior changes. Put a source-backed discovery in [findings](research/findings.md); record a live check's environment and outcome in [validation](research/validation.md). The [contribution workflow](developer/contributing.md) covers code and review checks.
