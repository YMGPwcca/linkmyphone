# Clipboard synchronization

[Documentation index](../README.md)

The clipboard module synchronizes plain text, HTML fragments and images through the CDEH/DCG clipboard.v1 publication and CONTENT exchange. Live checks on CachyOS/Wayland with a Samsung S23 covered HTML and image pastes in both directions; see [validation](../research/validation.md#clipboard-validation-2026-10-03). Rich formatting uses HTML fragments; arbitrary RTF and document formats are not supported.

## Providers

| Desktop | Provider | Content |
| --- | --- | --- |
| Wayland | wl-paste / wl-copy | Text, HTML, PNG/JPEG/GIF/BMP input |
| X11 | xclip | Text, HTML, PNG/JPEG/GIF/BMP input |
| X11 fallback | xsel | Plain text only |

Install the matching clipboard utilities. Python 3, PyGObject and GTK4 are optional: they let incoming HTML offer both its original fragment and derived plain text. Without them, the command provider offers HTML only, so text-only applications may not paste it. LinkMyPhone retains HTML as data and never executes it.

GIF uses its first frame. BMP support follows the decoder's 8/24/32-bit variants. HEIC, AVIF, TIFF and WebP are not supported. File selections and unknown MIME types are skipped.

Rich providers inspect MIME offers at `poll_interval_ms` (default 500 ms), preferring image, then HTML, then text. This catches formatting changes even when visible text is identical. The legacy text watcher remains available. A transient empty selection is debounced for 100 ms and re-read before a clear is published.

## Start and configure

```bash
linkmyphone feature create --enabled linkmyphone.clipboard
linkmyphone run
```

Enable an existing record rather than creating it twice. See [first run](../getting-started/first-run.md) for authentication and target selection, and [configuration](../reference/configuration.md) for commands and timing ranges.

`publish_initial` defaults to false: the clipboard present at startup is not sent. When enabled, supported initial content is published on every module start, including recovery. `request_timeout_ms` bounds sending and waiting for protocol responses. Oversized, malformed, unavailable or unsupported content is skipped with a diagnostic that contains no clipboard content; a later valid copy can continue syncing.

## Limits and ordering

Text and HTML must be valid UTF-8 and contain fewer than 131072 UTF-16 units; an emoji outside the BMP counts as two units.

Images received from the phone are converted to PNG without resizing. Their decoded dimensions are preserved even if PNG encoding exceeds 1 MiB; Android may already have reduced the image before sending it. Only Linux → phone images are resized when needed to fit 1048576 PNG bytes. Resizing may lose detail.

Incoming phone encodings and non-PNG native input are capped at 16 MiB. Desktop PNGs are capped at 128 MiB, and decoded dimensions at 33554432 pixels (32 Mi pixels). Valid native PNGs retain their exact bytes until outbound preparation.

Published snapshots retain the advertised type, bytes and timestamp for two minutes, with at most 64 entries and 16 MiB in aggregate. A matching CONTENT request receives that snapshot, not an unrelated new selection. Newer phone/local generations suppress stale writes; superseded correlations are rejected. Format-aware hashes suppress reflected copies using the full desktop representation, before outbound resizing.

There is no clipboard history or secret filter. Clipboard contents pass through Microsoft services; stop synchronization before copying secrets.

## Validate on your devices

Use harmless content and avoid copying secrets:

1. Copy text in both directions, including formatting, emoji and an empty selection.
2. Test HTML in a rich editor and a text-only app. Try supported image formats in both directions, including one image over 1 MiB.
3. Test a clear, an unsupported file followed by valid text, and a module restart.
4. Repeat after disconnecting a device or suspending Linux; wait for readiness before testing again.

For detailed recovery steps, see [session recovery](../operations/session-recovery.md). For automated and opt-in checks, see the [testing guide](../developer/testing.md).

## Image behavior compared with Windows

Phone → Linux keeps the dimensions Android sends. Linux → phone resizes PNG as needed to fit 1048576 bytes using area-average resampling; Windows uses high-quality bicubic resizing and a different size-selection rule. See [validation](../research/validation.md#clipboard-validation-2026-10-03) for measured results.
