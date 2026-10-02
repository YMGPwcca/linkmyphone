# Clipboard synchronization

[Documentation index](../README.md)

The clipboard module synchronizes plain text, HTML fragments and images through the existing CDEH/DCG clipboard.v1 publication and CONTENT exchange. This release adds rich content; authenticated HTML/image compatibility still requires a real phone test.

## Providers

| Desktop | Provider | Content |
| --- | --- | --- |
| Wayland | wl-paste / wl-copy | Text, HTML, PNG/JPEG/GIF input |
| X11 | xclip | Text, HTML, PNG/JPEG/GIF input |
| X11 fallback | xsel | Plain text only |

Install the matching clipboard utilities. Optional Python 3, PyGObject and GTK4 allow incoming HTML to offer both its original fragment and derived plain text. Without those dependencies, the command provider offers HTML only; applications requesting only plain text may not paste it. HTML is retained as data, never executed by this program. GIF uses its first frame. File selections and unknown MIME types are skipped.

Rich providers inspect MIME offers at `poll_interval_ms` (default 500 ms), preferring image, then HTML, then text. This catches formatting changes even when visible text is identical. The legacy text watcher remains available. A transient empty selection is debounced for 100 ms and re-read before a clear is published.

## Start and configure

```bash
linkmyphone feature create --enabled linkmyphone.clipboard
linkmyphone run
```

Enable an existing record rather than creating it twice. See [first run](../getting-started/first-run.md) for authentication and target selection, and [configuration](../reference/configuration.md) for commands and timing ranges.

`publish_initial` defaults to false: the clipboard present when the module starts is not sent. With it enabled, supported initial content is published once. `request_timeout_ms` bounds sending and waiting for protocol responses. Oversized, malformed, unavailable or unsupported content is skipped with a content-free diagnostic; a later valid copy can continue syncing.

## Limits and ordering

Text and HTML must be valid UTF-8 and contain fewer than 131072 UTF-16 units; an emoji outside the BMP counts as two units. Images are normalized to PNG and resized when necessary to fit 1048576 bytes. Local encoded image input is capped at 16 MiB and decoded dimensions at 32 million pixels. Resizing changes dimensions and may lose detail. PNGs already within the limit retain their exact bytes.

Published snapshots retain the advertised type, bytes and timestamp for two minutes, at most 64 entries and 16 MiB in aggregate. A matching CONTENT request receives that snapshot rather than an unrelated new selection. Newer phone/local generations suppress stale writes; superseded correlations are rejected. Format-aware hashes suppress reflected copies. There is no clipboard history or secret filter. Clipboard contents pass through Microsoft services; stop synchronization before copying secrets.

## Validate on your devices

Use non-sensitive samples in both directions:

1. Copy Vietnamese text, newlines, emoji and an empty string/clear.
2. Copy formatted HTML, then change only formatting. Paste into an HTML-capable editor and a text-only application.
3. Copy a transparent PNG, a JPEG, and an image exceeding 1 MiB. Check received pixels/dimensions and successful paste.
4. Copy files or unsupported content, then valid text; synchronization should recover without restarting.
5. Copy a new local selection while a phone CONTENT response is delayed; the delayed value must not overwrite the newer generation.
6. Stop/restart the module and verify initial publication follows configuration.

Deterministic tests cover these content contracts and simulated fragmented relay transfers. They do not establish authenticated cloud compatibility. Existing relay reconnect, sleep/wake recovery and active-session token refresh limitations still apply. See [testing](../developer/testing.md) and [privacy](../operations/privacy-and-state.md).
