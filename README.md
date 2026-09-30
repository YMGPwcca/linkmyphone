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
  - `SendMessageAsync`
  - `SendSessionBasedMessageAsync`
  - `OnReceiveMessage`
  - `OnReceiveSessionBasedMessage`
  - Hub Relay multiplex packets
  - minimal WebSocket transport
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

## Still required for an end-to-end usable client

- Microsoft account / DCG authentication bootstrap
- local and target `DcgClientId` discovery/provisioning
- connection/session bootstrap and wake behavior
- Linux native clipboard backend
- executable/daemon wiring and user configuration

## Test

```bash
gofmt -w .
go test ./...
```

GitHub Actions is currently configured as manual-only while the account Actions-minute quota is exhausted.
