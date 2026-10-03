# Clipboard synchronization

[Documentation index](../README.md)

The clipboard module synchronizes plain text, HTML fragments and images through the existing CDEH/DCG clipboard.v1 publication and CONTENT exchange. Plain text, Rich text & HTML clipboard, and Image clipboard are Supported. Live checks on CachyOS/Wayland with a Samsung S23 include received HTML and image pastes in both directions; see [validation](../research/validation.md#clipboard-validation-2026-10-03). Rich formatting uses HTML fragments; arbitrary RTF and document formats are not supported.

## Providers

| Desktop | Provider | Content |
| --- | --- | --- |
| Wayland | wl-paste / wl-copy | Text, HTML, PNG/JPEG/GIF/BMP input |
| X11 | xclip | Text, HTML, PNG/JPEG/GIF/BMP input |
| X11 fallback | xsel | Plain text only |

Install the matching clipboard utilities. Optional Python 3, PyGObject and GTK4 allow incoming HTML to offer both its original fragment and derived plain text. Without those dependencies, the command provider offers HTML only; applications requesting only plain text may not paste it. HTML is retained as data, never executed by this program. GIF uses its first frame. BMP support follows the decoder's 8/24/32-bit variants. HEIC, AVIF, TIFF and WebP are not supported. File selections and unknown MIME types are skipped.

Rich providers inspect MIME offers at `poll_interval_ms` (default 500 ms), preferring image, then HTML, then text. This catches formatting changes even when visible text is identical. The legacy text watcher remains available. A transient empty selection is debounced for 100 ms and re-read before a clear is published.

## Start and configure

```bash
linkmyphone feature create --enabled linkmyphone.clipboard
linkmyphone run
```

Enable an existing record rather than creating it twice. See [first run](../getting-started/first-run.md) for authentication and target selection, and [configuration](../reference/configuration.md) for commands and timing ranges.

`publish_initial` defaults to false: the clipboard present when the module starts is not sent. With it enabled, supported initial content is published on each module start, including recovery. `request_timeout_ms` bounds sending and waiting for protocol responses. Oversized, malformed, unavailable or unsupported content is skipped with a content-free diagnostic; a later valid copy can continue syncing.

## Limits and ordering

Text and HTML must be valid UTF-8 and contain fewer than 131072 UTF-16 units; an emoji outside the BMP counts as two units. Images received from the phone are converted to PNG without resizing: their decoded dimensions are preserved, even when PNG encoding exceeds 1 MiB. Android may already have reduced the image before sending it. Only Linux → phone images are resized when necessary to fit 1048576 PNG bytes. Incoming phone encodings and non-PNG native input are capped at 16 MiB; desktop PNGs at 128 MiB and decoded dimensions at 33554432 pixels (32 Mi pixels). Outbound resizing may lose detail. Valid native PNGs retain their exact bytes until outbound preparation.

Published snapshots retain the advertised type, bytes and timestamp for two minutes, at most 64 entries and 16 MiB in aggregate. A matching CONTENT request receives that snapshot rather than an unrelated new selection. Newer phone/local generations suppress stale writes; superseded correlations are rejected. Format-aware hashes suppress reflected copies using the full desktop representation, before outbound resizing. There is no clipboard history or secret filter. Clipboard contents pass through Microsoft services; stop synchronization before copying secrets.

## Validate on your devices

Use non-sensitive samples in both directions:

1. Copy Vietnamese text, newlines, emoji and an empty string/clear.
2. Copy formatted HTML, then change only formatting. Paste into an HTML-capable editor and a text-only application.
3. Copy a transparent PNG, a JPEG, and an image exceeding 1 MiB. Check received pixels/dimensions and successful paste.
4. Copy files or unsupported content, then valid text; synchronization should recover without restarting.
5. Copy a new local selection while a phone CONTENT response is delayed; the delayed value must not overwrite the newer generation.
6. Stop/restart the module and verify initial publication follows configuration.

Deterministic tests cover these content contracts and simulated fragmented relay transfers. They do not establish authenticated cloud compatibility. The owner reports live bidirectional clipboard use after automatic recovery from Linux/phone network loss and suspend/resume. Scheduled token-expiry renewal and multi-hour reliability still need separate live evidence. See [session recovery](../operations/session-recovery.md) for what happens to interrupted copies. See [testing](../developer/testing.md) and [privacy](../operations/privacy-and-state.md).

## Image behavior compared with Windows

Phone → Linux conversion preserves the dimensions Android sends, as Windows does on receive. A live comparison on 2026-10-03 pasted the same phone image into both Linux and Windows at **1572×2096**. This verifies dimensions for that sample, not pixel-for-pixel equality for all images.

Linux → phone sends PNG within **1048576 bytes (1 MiB)**. Linux uses independent area-average resampling and a different size-selection policy from Windows' high-quality bicubic path. In one owner-reported comparison of the same test image, Linux produced **583×1036** (displayed as 1.05 MB), while Windows produced **310×551** (displayed as 330 KB); the owner judged the Linux result better. Displayed file sizes are rounded, not exact wire-byte counts. This is a sample result, not a universal quality guarantee or exact Windows output parity.

To compare your own image, copy the same source file on both computers and paste into the same Android app without further recompression. Compare received dimensions, small text, fine edges and texture at the same displayed size. Inspect exact bytes if a file manager's rounded size appears to exceed 1 MiB.
