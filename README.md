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

- production MSA DCG scope (legacy MSA portal path):
  `service::msatoken.dcg.microsoft.com::MBI_SSL`
- first-party migrated scope:
  `https://dcg.microsoft.com/DCG.ReadWrite`
- services / SignalR access tokens are requested from the established DCG identity with scope `general`
- SignalR connection headers include DCG logical device/app/session/ring/OS metadata, tracing headers, optional partner id and hub region, and heartbeat frequency
- local DCG identity creation and token refresh are delegated to `AuthServiceCryptoHelper`, which performs signed-JWT based identity/sign-in flows

## Still required for an end-to-end usable client

- reverse and implement `AuthServiceCryptoHelper` signed identity/sign-in requests
- exact production configuration values such as service base URL, hub endpoint, MSA client/app ids
- target `DcgClientId` discovery/trust bootstrap
- Linux native clipboard backend
- executable/daemon wiring and user configuration

## Test

```bash
gofmt -w .
go test ./...
```

GitHub Actions is currently configured as manual-only while the account Actions-minute quota is exhausted.
