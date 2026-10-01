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

- live Microsoft/DCG integration validation of the new first-run bootstrap against a real account and linked phone
- minimum platform `/SessionValidation` / ContextSource bootstrap required before clipboard traffic
- Linux native clipboard backend
- executable/daemon wiring and user configuration
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

The remaining live validation point is reaching Hub Relay `OnConnected` after that transport fix.

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

## Test

```bash
gofmt -w .
go test ./...
```

GitHub Actions is currently configured as manual-only while the account Actions-minute quota is exhausted.
