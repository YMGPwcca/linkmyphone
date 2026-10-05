# Privacy and local state

[Documentation index](../README.md)

LinkMyPhone requires Microsoft cloud services. Sign-in uses Microsoft's identity service. Enrollment, linked-device discovery, trust synchronization, peer wake, SessionValidation, and feature traffic use DCG and SignalR. It is not an offline phone connection.

## Before you enable clipboard sync

The clipboard module has no content-based secret filter. Supported MIME selections are synchronized even if the source application labels them sensitive. Stop or disable the feature before copying secrets; sensitive-selection labels do not keep them local. See [`features/clipboard/sync.go`](../../features/clipboard/sync.go#L401-L415) for the handling.

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

The default feature registry is `~/.config/linkmyphone/features.json`. It stores desired feature IDs, enabled flags, and feature configuration. The built-in clipboard configuration contains timing values and `publish_initial`; it is not a clipboard history and does not contain clipboard contents.

The runtime control socket is a user-local Unix socket derived from the absolute feature-store path. It is created under `$XDG_RUNTIME_DIR/linkmyphone/` when the path fits the Unix socket limit, with a short `/tmp/linkmyphone-<uid>/` fallback otherwise. The directory is mode `0700` and socket mode is `0600`.

Authentication and feature files are written atomically. The state directory is mode `0700`; state and feature files are mode `0600`. These permissions are not encryption. LinkMyPhone does not claim encrypted-at-rest state, hardware-backed key storage, or protection from a compromised user account, root, or another process running as the same user.

## What clipboard sync sends

When supported local content changes, the clipboard module publishes it through the Microsoft DCG/SignalR clipboard protocol. When the phone publishes a change, the module requests its CONTENT payload and writes the returned text, HTML or PNG to the selected local provider.

## Account and device boundary

The account used for device-code sign-in determines which linked devices the service returns. Link to Windows / Phone Link must already contain the Windows PC and phone relationship. The Linux program can enroll its own DCG identity and refresh trust for returned peers, but it does not provide a new pairing wizard or guarantee compatibility with every Android device.

Deleting local state prevents subsequent runs from resuming this Linux identity at that path; a new bootstrap profile is then required. Uninstalling the systemd unit leaves authentication and feature state intact. Keep or delete those files separately according to your retention policy.

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

See the [source map](../developer/source-map.md) for implementation paths.

## Content held in memory

### Notifications

Android notification text, icons and action descriptors arrive through Microsoft's relay and are held in a bounded in-memory cache. LinkMyPhone does not persist notification history or log notification bodies, keys, icons or reply text. The desktop notification server, accessibility tools, receiving applications and crash handlers may retain their own copies.

Desktop dismissal and confirmed replies can affect the phone when `remote_actions` is true; set it false for receive-only use. Disabling the feature closes local notifications and cancels requests/windows, but never clears phone notifications.

A fresh PL enrollment adds one Microsoft account device/trust entry for both clipboard and notifications. Preserve an existing WEA identity when upgrading through a separate PL enrollment; changing `clientProfile` does not migrate it.

### Clipboard

Published snapshots hold text, HTML or outbound PNG for up to two minutes, with at most 64 entries and 16 MiB total. The native image cache retains the latest normalized desktop image, with a 128 MiB PNG budget and a 33554432-pixel limit on decoded images. These are separate from the outbound PNG limit of 1 MiB and are not a total process-memory ceiling: decoding, encoding and clones use additional memory.

A GTK clipboard helper may own received HTML until another selection replaces it. LinkMyPhone does not store clipboard history on disk; desktop clipboard managers and receiving apps may retain their own copies.
