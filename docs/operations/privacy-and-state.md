# Privacy and local state

[Documentation index](../README.md)

LinkMyPhone uses Microsoft cloud services. Sign-in uses Microsoft's identity service; enrollment, linked-device discovery, trust synchronization, peer wake, SessionValidation, and feature traffic use Microsoft's DCG and SignalR services. The program is not an offline phone connection.

## Before you enable clipboard sync

The clipboard module has no content-based secret filter. Supported MIME selections are synchronized even if the source application labels them sensitive. Stop or disable the feature before copying secrets. Do not rely on an application's sensitive-selection label to keep them local.

The module does not print clipboard contents in normal runtime, probe, or service output. Diagnostics report byte counts and shortened correlation IDs. A desktop clipboard provider, journal collector, crash handler, or other process may have separate logging behavior outside this program.

`publish_initial` controls whether the current local content is sent once at module startup; it defaults to false.

## What the program stores

The default authentication state is `~/.config/linkmyphone/state.json`. It stores:

- a Microsoft refresh token;
- the Linux logical device ID and DCG identity ID;
- DCG and trust private keys and their certificates;
- a DCG `general` service token and token metadata;
- enrollment account certificate and root certificate chain;
- account metadata and linked-device trust relationships.

The default feature registry is `~/.config/linkmyphone/features.json`. It stores desired feature IDs, enabled flags, and feature configuration. The built-in clipboard configuration contains timing values and `publish_initial`; it is not a clipboard history and does not contain clipboard text.

The runtime control socket is a user-local Unix socket derived from the absolute feature-store path. It is created under `$XDG_RUNTIME_DIR/linkmyphone/` when the path fits the Unix socket limit, with a short `/tmp/linkmyphone-<uid>/` fallback otherwise. The directory is mode `0700` and socket mode is `0600`.

Authentication and feature files are written atomically. The state directory is mode `0700` and state and feature files are mode `0600`. These are access permissions, not encryption. The implementation does not claim encrypted-at-rest state, hardware-backed key storage, or protection from a compromised user account, root, or a process running as the same user.

## What clipboard sync sends

When a local supported-content change is observed, the clipboard module publishes it through the Microsoft DCG/SignalR clipboard protocol. When the phone publishes a change, the module requests the associated CONTENT payload and writes the returned text, HTML or PNG to the selected local provider. `publish_initial` controls whether the current local content is sent once at module startup; it defaults to false.

There is no content-based secret filter. Sensitive-selection labels do not exclude content from synchronization. Stop or disable the feature before copying secrets; do not rely on an application's sensitive-selection label to keep them local. The handling is visible in [`features/clipboard/sync.go`](../../features/clipboard/sync.go#L401-L415).

## Account and device boundary

The account used for device-code sign-in determines which linked devices the service returns. Link to Windows / Phone Link must already contain the Windows PC and phone relationship. The Linux program can enroll its own DCG identity and refresh trust for returned peers, but it does not provide a new pairing wizard or guarantee compatibility with every Android device.

Removing local state is a destructive local operation: subsequent runs cannot resume this Linux identity from that path and will require a new bootstrap profile. Uninstalling the systemd unit does not remove authentication state or feature state. Keep or delete those files separately according to your retention policy.

## Safe bug reports

Prefer command output that names the failing stage without credentials:

```bash
linkmyphone feature get linkmyphone.clipboard
~/.local/bin/linkmyphone service logs --lines 100
```

Before sharing logs, redact refresh and access tokens, DCG service tokens, private keys, certificates, account certificate material, device IDs, peer names, account identifiers, hostnames, usernames, custom paths, and environment values that identify the machine. Remove every clipboard value. Do not attach `state.json` or paste its contents.

A useful report can retain:

- the command name and non-secret flags, with custom paths replaced by placeholders;
- Linux distribution and desktop protocol, such as Wayland or X11;
- the selected clipboard backend and whether the provider utilities are installed;
- the first error and the stage that emitted it;
- redacted service status and journal excerpts;
- byte counts, feature IDs, and module state when they do not identify a person or device.

If a maintainer requests a state fixture for a parser defect, create a separate copy, remove all credential and identifying fields, replace IDs and paths, and verify that no token or private key remains. Do not send live authentication state through a public issue tracker.

See [troubleshooting](troubleshooting.md) for stage-specific recovery and [configuration](../reference/configuration.md) for the complete stored-field reference.

The state implementation and permission tests are [`auth/state/store.go`](../../auth/state/store.go) and [`auth/state/store_test.go`]; clipboard transfer behavior is in [`clipboard/client.go`](../../clipboard/client.go) and [`features/clipboard/sync.go`](../../features/clipboard/sync.go).
