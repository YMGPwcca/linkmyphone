# Protocol research

[Documentation index](../README.md)

## Follow a message

The protocol docs follow the connection from Microsoft sign-in to a clipboard transfer. Start with the layer you are working on:

| Layer | Documentation |
| --- | --- |
| Account tokens, enrollment, device trust and wake | [Authentication and bootstrap](../protocol/authentication.md) |
| WebSocket, SignalR, DCG fragments and PLATFORM messages | [Transport and framing](../protocol/transport.md) |
| Clipboard publication, CONTENT requests and ordering | [Clipboard protocol](../protocol/clipboard.md) |
| Reconnect, token renewal and module restart | [Session resilience](../architecture/session-resilience.md) |

The [architecture overview](../architecture/overview.md) maps these layers to Go packages. The [glossary](../reference/glossary.md) explains protocol identifiers.

## Research notes

| File | Contents |
| --- | --- |
| [Notification sync](notification-sync.md) | Source investigation of Phone Link APP push/actions, CrossDevice GET and unresolved session prerequisites. No notification implementation. |
| [Findings](findings.md) | Problems found during implementation, what caused them and the fixes. |
| [Validation](validation.md) | Test commands, device results and remaining checks. |
| [Development history](history.md) | Commits in implementation order. |
| [Method and provenance](method.md) | Source inspection, capture procedure and how results were recorded. |

The repository contains the Go client, tests and research notes. Decompiled Windows/Android source and raw production captures are kept outside it. Follow the [privacy guide](../operations/privacy-and-state.md) when preparing an issue or capture.

## Recent tests

- [Clipboard, 2026-10-03](validation.md#clipboard-validation-2026-10-03): text, HTML and images on S23/Wayland, including incoming dimensions and Linux/Windows outbound image comparisons. [Clipboard content](clipboard-content.md) explains the encoding and resize rules.
- [Session recovery, 2026-10-03](validation.md#session-resilience-2026-10-03): full race suite, Linux and phone network loss, suspend/resume and saved feature state. The [recovery guide](../operations/session-recovery.md) covers the remaining token-expiry and extended-run checks.
