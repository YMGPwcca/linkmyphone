# phonelink-linux

Experimental clean-room interoperability work for Microsoft Phone Link / Link to Windows, with an initial focus on clipboard synchronization.

## Current scope

The first implementation target is an offline Go codec for the clipboard protocol. Network transport, authentication, and pairing are intentionally separate layers.

Implemented now:

- `ClipboardRequestMessage`
- `ClipboardResponseMessage`
- `ClipboardItem`
- minimal protobuf `Timestamp`
- PubSub two-field payload wrapper
- Device Resource Manager wrapper for `/clipboard`
- helpers for `CLIPBOARD_CHANGE`, `STATUS`, `CONTENT`, `FEATURE_ON`, and text responses
- protobuf-compatible marshal/unmarshal without external dependencies

Known protocol constants currently represented in code:

- clipboard resource path: `/clipboard`
- DRM request type for clipboard reads: `GET = 1`
- generic DRM resource type used by clipboard: `UNKNOWN = 4`
- clipboard change response status: `6`
- feature-on response status: `8`

## Test

```bash
go test ./...
```

The package currently lives at:

```text
protocol/clipboard
```

Transport over PubSub / DeviceResourceManager / DCG / SignalR is the next layer.
