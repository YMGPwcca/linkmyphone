# Protocol research

[Documentation index](../README.md)

## Follow a message

The protocol references trace a connection from Microsoft sign-in to clipboard transfer. Choose the layer you need:

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
| [Notification sync](notification-sync.md) | Historical source investigation of Phone Link APP push/actions, CrossDevice GET and session prerequisites; later implementation results are linked from the page. |
| [Clipboard content](clipboard-content.md) | Content encoding, image dimensions, transfer limits and source provenance. |
| [Findings](findings.md) | Implementation failures, causes, fixes and evidence. |
| [Validation](validation.md) | Test commands, dated device results and remaining checks. |
| [Development history](history.md) | Commits in implementation order. |
| [Method and provenance](method.md) | Source inspection, capture procedure and evidence records. |

The repository contains the Go client, tests and research notes. Decompiled Windows/Android source and raw production captures are kept outside it. Follow the [privacy guide](../operations/privacy-and-state.md) when preparing an issue or capture.

## Recent tests

- [Clipboard, 2026-10-03](validation.md#clipboard-validation-2026-10-03): text, HTML and images on S23/Wayland, including incoming dimensions and Linux/Windows outbound image comparisons. [Clipboard content](clipboard-content.md) explains the encoding and resize rules.
- [Session recovery, 2026-10-03](validation.md#session-resilience-2026-10-03): full race suite, Linux and phone network loss, suspend/resume and saved feature state. The [recovery guide](../operations/session-recovery.md) covers the remaining token-expiry and extended-run checks.
- [Notifications and unified enrollment, 2026-10-05](validation.md#notification-desktop-actions-on-unified-enrollment-2026-10-05): clipboard and notifications Ready after one PL sign-in; Messenger Like, reply and desktop dismissal on S23/CachyOS/Wayland.
