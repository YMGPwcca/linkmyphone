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

## Still required for an end-to-end usable client

- target `DcgClientId` discovery/trust bootstrap
- Linux native clipboard backend
- executable/daemon wiring and user configuration

## Test

```bash
gofmt -w .
go test ./...
```

GitHub Actions is currently configured as manual-only while the account Actions-minute quota is exhausted.
