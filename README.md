# phonelink-linux

Clean-room Go implementation targeting Microsoft Phone Link / Link to Windows interoperability on Linux, currently focused on clipboard sync.

## Implemented

The repository now includes:

- clipboard protobuf-compatible codecs:
  - `ClipboardRequestMessage`
  - `ClipboardResponseMessage`
  - `ClipboardItem`
  - `PubSubPayload`
  - Device Resource Manager request/response wrappers
- clipboard state machine:
  - `STATUS`
  - `CONTENT`
  - `FEATURE_ON` / `FEATURE_OFF` / `FEATURE_DISABLE`
  - `CLIPBOARD_CHANGE`
  - Windows-style phone change -> immediate CONTENT pull
- Platform binary framing:
  - `/DeviceResourceManager`
  - `/internal/response`
  - `/Context/Publish`
- MSAEP cloud PubSub envelope:
  - message tag
  - DCG client id
  - message id
  - payload
  - platform protocol version 1.1
- DCG:
  - fragment encode/decode
  - reassembly
  - ACK handling
  - retry/timeout
  - source-confirmed message type values
- SignalR / Hub Relay:
  - MessagePack Hub Protocol framing
  - normal PLATFORM traffic uses `SendMessageAsync`
  - session-based Hub Relay methods remain modeled separately from DCG `SessionId`
  - `OnReceiveMessage` / `OnReceiveSessionBasedMessage`
  - Hub Relay multiplex packets
  - minimal WebSocket transport
- DCG session identity:
  - stable UUID per target DCG client for the lifetime of the relay client
  - independent from SignalR `connectionSessionId`
- clipboard cloud publication:
  - PC clipboard change -> `PubSubPayload.Data`
  - MSAEP tag 9
  - one-way `/Context/Publish`
- phone clipboard publication receive path:
  - incoming MSAEP tag 9
  - parse `PubSubPayload.Additional`
  - request `CONTENT`
  - apply text to the local clipboard backend
- native Linux text clipboard backend:
  - Wayland via `wl-paste` / `wl-copy`
  - X11 fallback via `xclip` or `xsel`
  - direct process execution without a shell
- modular runtime:
  - business-agnostic `runtime/kernel` for strict manifests, lifecycle, dependencies, epochs, live capabilities, and desired-state CRUD
  - shared `runtime/phonehost` owns authentication, trust, wake, SessionValidation, and the sole raw relay receive loop
  - feature-scoped bounded message endpoints prevent modules from stealing each other's relay traffic
  - build composition is isolated in `features/catalog.go`
  - persisted feature state defaults to `~/.config/phonelink-linux/features.json`
- `phonelink.clipboard` feature module:
  - owns the native Linux clipboard, clipboard protocol client, workers, generation conflict arbitration, and diagnostics
  - advertises live text read/write/bidirectional capabilities only while Ready
  - publishes Linux clipboard changes and applies phone clipboard publications
  - suppresses immediate phone -> Linux -> phone echo loops
  - snapshots outbound text by correlation id so CONTENT replies match the advertised change
  - uses one cross-device generation domain so newer observed local/phone changes supersede stale protocol work deterministically
- `clipboard-sync` remains as a compatibility alias that starts the same `phonelink.clipboard` module through the modular lifecycle

## Source-confirmed clipboard cloud path

```text
local PC clipboard change
    -> ClipboardResponseMessage(CLIPBOARD_CHANGE)
    -> PubSubPayload.Data
    -> MsaepMessage(tag=9)
    -> PLATFORM /Context/Publish
    -> DCG PLATFORM fragments
    -> Hub Relay SignalR

phone clipboard change
    -> Hub Relay
    -> DCG PLATFORM reassembly
    -> PLATFORM /Context/Publish
    -> MsaepMessage(tag=9)
    -> PubSubPayload.Additional
    -> CONTENT GET /clipboard
    -> local clipboard
```

## Source-confirmed authentication details

- production service base: `https://dcg.microsoft.com/`
- CrossDevice public MSA client/application id: `ca3b40e4-3001-4842-8f21-49c0045404f8`
- Windows acquires the MSA token only through WAM: provider `https://login.microsoft.com`, authority `consumers`, `WebTokenRequest(clientId=MSAClientId)` and `GetTokenSilentlyAsync`
- WAM formats the requested DCG scope as `<scope>&api-version=2.0&clientid=<MsaAppId>`; this is WAM-specific and is not treated as a generic OAuth v2 scope by this implementation
- if WAM reports `UserInteractionRequired` or `AccountSwitch`, YPP surfaces that status upward; the decompiled YPP layer does not contain an interactive sign-in fallback
- Linux interoperability path: the same public client id with scope `https://dcg.microsoft.com/DCG.ReadWrite offline_access` was runtime-confirmed against `/consumers/oauth2/v2.0/devicecode` and returned HTTP 200
- `auth/msa` implements the Microsoft identity-platform device-code flow plus token polling and refresh-token support for that client/scope
- `auth/bootstrap` connects an acquired MSA access token directly to the source-confirmed DCG identity bootstrap and exposes the resulting DCG `general` services token
- `auth/dcgauth` now models device enrollment: `POST /DeviceAuthProxy/EnrollDevice?api-version=1.5.0`, MSA bearer + `Dcg-Token`, Windows-compatible WEA metadata, and the separate `trust_<dcgClientId>` certificate registered as `SelfSigned`
- default relay hub endpoint: `relayhub/`
- production MSA DCG scope (legacy MSA portal path):
  `service::msatoken.dcg.microsoft.com::MBI_SSL`
- first-party migrated scope:
  `https://dcg.microsoft.com/DCG.ReadWrite`
- services / SignalR access tokens are requested from the established DCG identity with scope `general`
- identity bootstrap uses auth API version `1.1.0`
- identity creation is nonce based:
  - generate a random DCG device id
  - fetch a nonce for that device id
  - generate an ECDSA P-384 self-signed certificate with CN=<device id>
  - sign a JWT containing `Nonce` and base64 DER `Certificate` using ES384
  - submit that JWT to CreateIdentity and persist the certificate on success
- sign-in repeats the nonce challenge using the persisted certificate and returns a scoped DCG access token
- default JWT clock drift and lifetime are 12 hours; default identity certificate lifetime is 365 days
- SignalR connection headers include DCG logical device/app/session/ring/OS metadata, tracing headers, optional partner id and hub region, and heartbeat frequency
- `auth/dcgauth` contains the source-confirmed production constants, ECDSA/JWT identity primitives, and auth-service HTTP client
- source-confirmed auth routes:
  - `POST /Auth/GenerateNonce?api-version=1.1.0` with `{\"deviceId\": ...}`
  - `POST /Auth/CreateIdentity?api-version=1.1.0` with `{\"certificateJWT\": ...}`
  - `POST /Auth/SignIn?api-version=1.1.0` with `{\"certificateJWT\": ...}`
  - `POST /Auth/RotateKeys?api-version=1.1.0`
- auth requests carry `UserIdentityType: MSA`, `UserIdentityToken`, `Authorization: Bearer <MSA token>`, and `Authorization-Type: MSA`
- peer discovery uses `GET /DeviceAuthProxy/GetDeviceInfoList?api-version=1.5.0&top=20&filter=` with the MSA bearer token; linked entries other than self provide target DCG client ids
- async trust maps a DCG id to `trust_<dcgClientId>` and uses peer PKI/SelfSigned certificates from device metadata
- enrollment account certificate is imported into a local A2D relationship; linked peer metadata becomes AsyncD2D trust with production `dcg_prod_client_id` mapping
- persistent Linux auth state stores the MSA refresh token, DCG/trust private keys and certificates, DCG `general` token, account certificate/root chain, trust relationships, and a stable logical-device id in a `0600` state file
- normal restart uses refresh-token acquisition plus `/Auth/SignIn`; it does not create a new DCG identity
- account-level SignalR bootstrap fetches assigned shards, connects `relayhub/` using the DCG `general` token and exact DCG headers, waits up to 10 seconds for `OnConnected`, and retries another shard on resolved-region mismatch
- first-run `bootstrap.BootstrapFirstRun` now wires enrollment -> trust sync -> persistence -> account SignalR connection as one pipeline

## Still required for an end-to-end usable client

- event-driven Wayland clipboard watching instead of polling
- service/daemon packaging
- reconnect/wake/token-refresh hardening for long-running parity with Phone Link

## Production bootstrap validation

The first live Linux bootstrap probe against the Microsoft production services has confirmed:

- Microsoft device-code login succeeds for the CrossDevice public client and migrated DCG scope;
- DCG identity creation and device enrollment succeed;
- persistent state is written before later cloud stages;
- DeviceInfoList returns linked Windows and Android peers and trust sync succeeds;
- the account receives an assigned SignalR shard;
- the production SignalR service can return its JSON handshake response in a **binary WebSocket message**, so the transport accepts both text and binary handshake messages and preserves any first Hub payload coalesced after the record separator.
- DCG Hub Relay fragment `MessageType` uses the internal Windows `TransportMessageType` enum (`App=0`, `Platform=1`, `Unknown=2`), not the protobuf enum (`App=1`, `Platform=2`); the Linux transport now uses the Hub values in both directions.
- Windows sends Hub Relay packets with SignalR `InvokeAsync`, so Linux now tracks the matching Hub `Completion` for each DCG fragment send; diagnostics distinguish a Hub rejection from a Hub-accepted packet that never receives a peer DCG ACK.
- Windows' wake path performs a pre-wake `SendConnectedAsync` flush and waits for its Hub Completion before Dispatcher/Wake; Linux now mirrors that ordering instead of waiting until `OnPartnerConnected` to send reciprocal presence.
- Windows sends every Hub Relay operation with a non-null Hub Relay trace context (`TraceId` 32 hex chars, `ParentId` 16 hex chars, non-null `TraceState`); Linux now generates/normalizes the same shape for fragment, ACK, and partner-presence sends. An empty trace object can make the receiver fail before DCG packet processing and therefore before it emits an ACK.
- with that trace shape fixed, the production S23 accepts a Linux PLATFORM `/SessionValidation` request and returns `/internal/response` with `SessionValidation`, `PersistentMessageChannel`, and `NanoTransportPreference`; the observed versions were PersistentMessagingChannel 14 and NanoTransportPreference 3.
- a production S23 accepts the Linux clipboard tag-9 `/Context/Publish` publication and issues `GET /clipboard` using the published `CLIPBOARD_CHANGE` correlation id;
- the observed S23 state machine advances through `STATUS -> FEATURE_ON -> CONTENT` when STATUS is present, and can also request CONTENT directly on a later run;
- returning a successful text/plain CONTENT response was live-validated end to end: the explicit Linux probe text appeared in the S23 clipboard and was pasteable on the phone;
- continuous native text synchronization was live-validated in both directions on Wayland with an S23;
- the `wl-copy` fork/pipe latency bug was fixed, phone-to-Linux logging became immediate, and reflected clipboard echo was eliminated;
- the pre-modular continuous path passed both `go test ./...` and `go test -race ./...`;
- the modular `feature create --enabled` + `run` path was live-validated end to end on the S23: the shared phone host reached SessionValidation, `phonelink.clipboard` reached Ready, live capabilities were published, two-way clipboard traffic remained functional, and Ctrl+C performed a clean module shutdown;
- live control-plane CRUD was validated end to end without restarting the runtime process: `list/get` returned live state, `disable` transitioned Ready -> Stopped and revoked the module, `enable` started it again, a live config update stopped/restarted the module and increased its epoch, `delete` removed the running module while the shared Phone Link session stayed alive, and `create --enabled` instantiated a fresh Ready module again;
- after delete + create, the new module entry starts a fresh epoch sequence at 1 by design; this is distinct from restarting the same registry entry, where the epoch increases monotonically;
- the live runtime control socket was observed at the hashed XDG path (`/run/user/<uid>/phonelink-linux/<store-hash>.sock`) and the entire disable -> enable -> config update -> delete -> create sequence completed while the original `phonelink-linux run` process remained connected to the same S23;
- final Ctrl+C after the live CRUD sequence shut the recreated clipboard module down cleanly with `module stopped`;
- the modular/control-plane branch passed targeted stress tests plus full `go test ./...` and `go test -race ./...` after the lifecycle, router, generation, Unix-socket, and live-reconcile changes.

The modular runtime and live feature CRUD path are now validated end to end. Remaining runtime work is focused on event-driven local clipboard observation, service packaging, and long-running reconnect/token-refresh resilience.

## Bootstrap probe

The repository includes an interactive production probe for bootstrap stages 1-4:

```bash
go run ./cmd/phonelink-linux bootstrap-probe
```

On first run it:

1. starts Microsoft device-code login with the runtime-confirmed CrossDevice public client and migrated DCG scope;
2. creates and enrolls one persistent DCG identity;
3. persists the identity/trust keys and refresh token before later network stages;
4. refreshes linked-device trust from DeviceInfoList;
5. resolves an assigned SignalR shard and waits for Hub Relay `OnConnected`.

Subsequent runs load the existing state, refresh the Microsoft token, call DCG `SignIn` with the same identity, refresh peer trust, and reconnect SignalR. They do not call `CreateIdentity` again.

The default state path is the platform user-config directory, normally:

```text
~/.config/phonelink-linux/state.json
```

The state file is written with mode `0600` and its directory with `0700`. It contains private key material and refresh credentials, so do not share it.

The compatibility profile defaults to the reverse-engineered CrossDevice build used for this work:

```text
app version: 1.26072.116.0
ring:        Public
OS version:  10.0.26100
```

These can be overridden with `--app-version`, `--ring`, and `--os-version` if runtime validation shows a deployment-specific requirement.

## Peer presence / wake probe

After `bootstrap-probe` has created persistent state, the next production probe is:

```bash
go run ./cmd/phonelink-linux peer-probe
```

The command:

- refreshes the Microsoft token and signs the existing DCG identity back in;
- refreshes linked-device trust;
- selects the sole linked Android device by default (or accepts `--target`);
- connects the account-level Hub Relay;
- if the target is not already present, sends the signed `Dispatcher/Wake` payload using the persisted `trust_<localDcgClientId>` identity;
- waits for `OnPartnerConnected` / peer traffic to mark the target online.

Useful overrides:

```bash
go run ./cmd/phonelink-linux peer-probe --target "Pwcca's S23"
go run ./cmd/phonelink-linux peer-probe --wake-timeout 60s
```

This probe does not send PLATFORM SessionValidation or clipboard traffic yet.

## PLATFORM SessionValidation probe

After `peer-probe` has confirmed that the Android target can be woken onto Hub Relay, run:

```bash
go run ./cmd/phonelink-linux session-probe
```

The probe reuses the same persisted identity, refreshes trust, ensures the linked Android peer is present, then sends the source-confirmed PLATFORM request:

```text
route: /SessionValidation
ms-content-type: application/x-binary
_rejectionVersion: 1
_requestId: <numeric request id>

protobuf request:
  PlatformCapabilities = [SessionValidation]
```

The baseline request advertises only `SessionValidation`, matching the unconditional capability in Windows' `PlatformCapabilitiesProvider`. Optional PersistentMessageChannel and NanoTransportPreference capabilities are feature-flagged in Windows and are not advertised by this probe.

A successful peer response is matched through `/internal/response` + `_originalRequestId` and decoded as:

```text
PlatformCapabilities
PersistentMessagingChannelVersion
NanoTransportPreferenceVersion
```

The CLI prints only capability/version information and platform header names; it does not dump the raw platform payload.

### ContextSource clipboard probe

After SessionValidation is working, enable the opt-in ContextSource probe:

```bash
go run ./cmd/phonelink-linux session-probe --context-probe
```

The extra probe sends the source-confirmed Windows PC clipboard publication shape without sending clipboard content:

```text
PLATFORM /Context/Publish
  -> MSAEP message tag 9
  -> PubSubPayload.Data
  -> ClipboardResponseMessage(CLIPBOARD_CHANGE)
```

The production S23 has now confirmed the first Android reaction:

```text
/DeviceResourceManager
  resource: /clipboard
  request: GET
  clipboard request: STATUS
  correlation: same id as CLIPBOARD_CHANGE
```

The probe now answers that STATUS exactly like the normal Windows-side clipboard resource handler:

```text
DeviceResourceResponse: Success
ClipboardResponse: FEATURE_ON
correlation: same id as STATUS
```

It then keeps the PLATFORM receive loop alive and waits for the next clipboard request. If the S23 advances to `CONTENT`, the probe records it and replies with `ResourceHandlerNotRegistered` so no real clipboard content is returned and neither clipboard is intentionally modified.

Useful override:

```bash
go run ./cmd/phonelink-linux session-probe --context-probe --context-timeout 15s
```

To validate the final PC-to-phone CONTENT response without wiring the Linux clipboard backend yet, explicitly provide non-sensitive probe text:

```bash
go run ./cmd/phonelink-linux session-probe \
  --context-probe \
  --context-timeout 15s \
  --context-text "phonelink-linux probe"
```

When Android requests CONTENT, the probe returns a normal successful text/plain clipboard response with the same correlation id. The CLI reports only the byte count, not the text itself. Probe text is limited to 4096 bytes and the option is rejected unless `--context-probe` is also present. Because command-line arguments may be stored in shell history, do not use sensitive text.

A timeout after the DCG acknowledgement is recorded as an observation rather than a transport failure. If STATUS was already observed, the result also records that FEATURE_ON was sent and that no CONTENT follow-up arrived before the probe timeout.

## Modular feature runtime

Feature manifests and lifecycle rules are documented in [`docs/architecture/MODULE_SYSTEM.md`](docs/architecture/MODULE_SYSTEM.md). Version 1 supports built-in modules with strict manifests; the kernel intentionally contains no clipboard-specific behavior.

Inspect available and installed features:

```bash
go run ./cmd/phonelink-linux feature list
go run ./cmd/phonelink-linux feature get phonelink.clipboard
```

Create an enabled clipboard feature record using its default configuration:

```bash
go run ./cmd/phonelink-linux feature create --enabled phonelink.clipboard
```

Replace its configuration:

```bash
go run ./cmd/phonelink-linux feature update \
  --config '{"poll_interval_ms":250,"request_timeout_ms":10000,"publish_initial":false}' \
  phonelink.clipboard
```

Desired-state CRUD is generic:

```bash
go run ./cmd/phonelink-linux feature disable phonelink.clipboard
go run ./cmd/phonelink-linux feature enable phonelink.clipboard
go run ./cmd/phonelink-linux feature delete phonelink.clipboard
```

Then start all enabled modules over one shared Phone Link host session:

```bash
go run ./cmd/phonelink-linux run
```

The same commands work both offline and against a running daemon. When `run` is active, it owns a versioned Unix control socket keyed by the absolute feature-store path. The preferred location is `$XDG_RUNTIME_DIR/phonelink-linux/<store-hash>.sock` (mode `0600`); if that path would exceed the Unix socket path limit or `XDG_RUNTIME_DIR` is unavailable, it falls back to `/tmp/phonelink-linux-<uid>/<store-hash>.sock`. The CLI sends CRUD through that socket and the runtime reconciles persistence plus kernel lifecycle immediately. If no daemon owns the socket, the CLI falls back to offline desired-state edits.

During live updates, enabled modules are stopped before configuration changes and restarted afterward. Enable/disable maps directly to Start/Stop, delete revokes the live module before removing persistence, and failed persistence triggers runtime rollback. The socket exists throughout daemon startup so commands cannot silently fall back offline while the process is still booting.

If a feature implementation is removed from the build, a stale disabled record remains readable, disable-able, and delete-able. It cannot be enabled or reconfigured until the implementation is present again.

## Continuous Linux clipboard sync

The recommended path is the modular `feature create --enabled` + `run` flow above. For compatibility and focused testing, the old command name still starts exactly the same module:

```bash
go run ./cmd/phonelink-linux clipboard-sync
```

The module auto-detects a native Linux text clipboard provider. On Wayland it prefers `wl-clipboard`; on X11 it falls back to `xclip` and then `xsel`. Install at least one supported provider before running the command.

Startup does **not** publish the clipboard that was already present before the process started. Only subsequent local clipboard changes are published by default. To intentionally send the current clipboard immediately:

```bash
go run ./cmd/phonelink-linux clipboard-sync --publish-initial
```

The local clipboard is polled every 500 ms by default:

```bash
go run ./cmd/phonelink-linux clipboard-sync --poll-interval 250ms
```

Clipboard contents are never printed by the command; diagnostics report only direction, byte count, and shortened correlation ids.

Outbound text is snapshotted by correlation id. If the Linux clipboard changes again before Android requests CONTENT, the older request still receives the exact text associated with its own publication instead of the newer clipboard value.

Phone-originated writes are protected by an origin barrier before the native clipboard mutation begins. During the short compositor-settle window, both the previous local value and the incoming remote value are suppressed from outbound publication, closing the poll-vs-`wl-copy` race.

Android can also reflect a desktop-originated clipboard value back through its own publication path. Before applying phone CONTENT, the client compares it with the current Linux clipboard and skips an identical value entirely, so reflected desktop text does not produce a fake `phone -> Linux` event.

Outbound `/Context/Publish` calls run on a separate publisher worker. Waiting for a DCG ACK therefore no longer blocks receive-side logging or phone-to-Linux clipboard handling. Pending rapid local changes are coalesced to the newest text while one publication is in flight.

On Wayland, `wl-copy` forks by default to keep serving the selection. Native writes therefore use `exec.Cmd.Run` without captured stdout/stderr pipes; capturing those descriptors can leave the Go process waiting on the forked clipboard owner until the selection changes again. Echo tracking also treats CRLF/LF and a single terminal newline as equivalent during the remote-write suppression window, while preserving the original clipboard bytes for protocol payloads.

## Test

```bash
gofmt -w .
go test ./...
go test -race ./...
```

GitHub Actions is currently configured as manual-only while the account Actions-minute quota is exhausted.
