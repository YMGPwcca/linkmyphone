# Glossary

[Documentation index](../README.md)

Use the groups below as an index. Terms with a protocol or lifecycle contract link to the page that carries the detailed rules.

## Accounts and trust

| Term | Meaning in this client |
| --- | --- |
| MSA | Microsoft account. The Linux sign-in flow uses the personal-account `consumers` authority. |
| Device-code flow | Sign-in in which the CLI prints a verification URL and code while polling for tokens. It avoids a local browser callback. |
| WAM | Windows Web Account Manager. Its token-acquisition path and scope formatting differ from the Linux device-code flow. |
| DCG identity | The persisted client identity authenticated with a self-signed ECDSA certificate and nonce-signed JWT. |
| DCG `general` token | A scoped service token obtained for an established DCG identity; distinct from the Microsoft access and refresh tokens. |
| Trust identity | The separate `trust_<dcgClientId>` identity and certificate used for enrolled trust and signed wake. |
| A2D | The account-to-device trust relationship imported from the enrollment account certificate. |
| AsyncD2D | The peer trust relationship built from linked-device metadata and peer certificates. |
| Compatibility profile | The advertised CrossDevice app version, ring, and Windows-compatible OS version. These are service metadata, not the Linux or Android OS version. |

See [authentication and bootstrap](../protocol/authentication.md) for routes, token lifetimes, certificate handling, and trust construction.

## Connection and delivery

| Term | Meaning in this client |
| --- | --- |
| DCG | The Microsoft service and packet/identity layer used for discovery, enrollment, wake, and relay delivery. |
| Hub Relay | The account relay reached through SignalR; its callbacks carry partner presence and multiplexed DCG packets. |
| SignalR | The RPC layer over the WebSocket connection. This client uses a JSON handshake followed by MessagePack Hub messages. |
| Hub `Completion` | The result of a SignalR invocation identified by its invocation ID. It does not establish successful clipboard handling by the phone. |
| DCG ACK | The peer acknowledgement for a DCG delivery exchange, separate from the Hub completion and application response. A successful ACK can complete a send before Hub `Completion`; a negative ACK, rejection, or cancellation fails without retry. |
| PLATFORM | The binary route/header/payload envelope carried inside DCG PLATFORM traffic. The capitalization distinguishes the wire layer from a host operating system. |
| MSAEP | The message envelope around clipboard PubSub payloads, carrying the message tag, client ID, message ID, protocol version, and payload. Clipboard uses tag 9. |
| DRM | Device Resource Manager. `/DeviceResourceManager` carries requests for the `/clipboard` resource; responses use `/internal/response`. |
| SessionValidation | The PLATFORM exchange that requests and reports peer platform capabilities before feature traffic. |

See [transport and framing](../protocol/transport.md) for packet layouts, routing, acknowledgement stages, and errors.

## Identifiers that must stay separate

| Identifier | Scope and purpose |
| --- | --- |
| DCG client ID | Identifies the local or peer DCG client for service and relay operations. |
| Logical-device ID | Persisted metadata identifying the logical local device across restarts. |
| DCG `SessionId` | The default is a stable per-target UUID for a relay client's lifetime. A relay API caller can instead supply a non-empty session ID. |
| Hub `connectionSessionId` | An argument to separately modeled session-based Hub methods. It is not the DCG `SessionId`. |
| SignalR invocation ID | Matches a Hub invocation to its `Completion`. |
| PLATFORM `_requestId` | Identifies a routed PLATFORM request. A response echoes it as `_originalRequestId`. |
| Clipboard correlation ID | Ties a change advertisement, STATUS/CONTENT requests, and responses to the relevant clipboard exchange. |
| Clipboard generation | The local ordering sequence shared by observed Linux and phone changes. It protects newer observations from stale work; it is not a globally synchronized device clock. |
| Module epoch | Identifies successive starts of one kernel registry entry. Recreating a deleted entry starts a fresh sequence. |

The [clipboard protocol reference](../protocol/clipboard.md) explains how correlation and generation work together.

## Runtime and desktop integration

| Term | Meaning in this client |
| --- | --- |
| Feature definition | A built-in manifest and factory included through `features/catalog.go`. |
| Feature record | Persisted desired state: feature ID, enabled flag, and configuration. A record can exist without a running instance. |
| Potential capability | A capability declared by a feature manifest. It is not evidence that the feature is running. |
| Live capability | A capability registered during successful instance startup and revoked when its ownership ends. Capability and lifecycle snapshots use separate synchronization, so they are not an atomic combined view. |
| Phone host | The shared owner of bootstrap, trust, wake, session validation, relay execution, and message routing. |
| Scoped endpoint | A revocable feature subscription with a matcher and bounded receive queue. Features do not compete for the raw relay channel. |
| Control plane | The local, versioned JSON request/response protocol over a Unix socket used by `feature` commands to reconcile a running runtime. |
| Auth state | `state.json`: enrollment, credentials, keys, certificates, trust, and account metadata. Treat it as a secret. |
| Feature store | `features.json`: installed feature records and configuration. It is separate from authentication state. |
| Native observer | The desktop source of clipboard-change events, MIME polling on rich backends; the legacy plain-text path can use `wl-paste --watch` on Wayland. |
| Polling fallback | Repeated native reads used on X11 or when Wayland watching is unavailable or fails. |
| Nil debounce | The 100 ms delay used to distinguish a genuine Wayland selection clear from a transient ownership handoff. |
| Echo suppression | Avoiding a phone-originated or reflected clipboard update being published back as a new Linux change. The origin barrier tracks previous and incoming format-aware content hashes for three seconds; plain-text comparison normalizes line endings; it does not filter sensitive clipboard content. |
| Graphical-session service | A systemd user unit enabled under and coupled to `graphical-session.target`. It follows the desktop login session. |

For lifecycle, dependency, capability, and snapshot rules, see [architecture](../architecture/overview.md) and [feature modules](../architecture/modules.md). For socket reconciliation, see [control plane](../architecture/control-plane.md). For stored options, see [configuration](configuration.md), and for operational clipboard behavior, see [clipboard behavior](../user-guide/clipboard.md).
