# Clipboard content implementation evidence

This change uses owner-supplied reverse-engineering observations as interoperability evidence. The implementation is independently written Go and a small Python GTK provider; it does not import decompiled application classes or proprietary assets into the runtime. This is not a claim of a clean-room process or a legal clearance.

The observed clipboard.v1 item has IMAGE=1, TEXT_PLAIN=2 and TEXT_HTML=3, optional text field 2, image bytes field 3 and timestamp field 4. Explicit empty text field presence matters. Windows ClipboardDrmCallBack and ClipboardSyncService select bitmap, HTML fragment, then Unicode text. Android ClipboardMessageBuilder limits text to fewer than 131072 UTF-16 units. Android ClipboardUtility may supply original JPEG/GIF bytes; therefore inbound images are decoded and normalized. The observed wire image budget is 1048576 bytes.

Source archive SHA-256 identifiers:

- Windows: ad55af430fb0d1760e1d8793a349608a92f6797633daa65d2bcc48c64704d2e5
- Android: 8022cbc1f2c1b60b17b8fc30405d3d5339e61c31fe2597383973179de40676ce

`clipboard/content.go` implements validation, conversion and an independently chosen area-average resizing algorithm using Go's standard image codecs and the Go project's BSD-licensed BMP decoder. `clipboard/native_content.go` discovers MIME targets with existing clipboard tools. `html_offer.py` uses GTK's public content-provider API to offer HTML and plain text together. Handwritten protocol codecs and existing DCG fragment/reassembly remain in use. The BMP decoder dependency is `golang.org/x/image` v0.30.0, compatible with Go 1.23; TIFF and WebP decoders are not imported. See `THIRD_PARTY_NOTICES.md`.

Regression evidence includes explicit-empty protobuf fixtures, typed correlated immutable snapshots, fragmented PNG exchanges, size and malformed-content rejection, UTF-16 limits, MIME preference, HTML/image echo suppression and an opt-in real X11 integration test. Real authenticated HTML/image exchange with Link to Windows has not yet been validated for this release.

Private storage controls access; it does not itself grant rights to reverse-engineered materials. The research corpus is separate from the independently written runtime and is not relicensed by the root MIT license. Any future public export should include only reviewed implementation and retain provenance/license notices. Existing first-party OAuth identifiers and service headers are unchanged and remain a separate compatibility and permission issue; this work does not resolve them.
