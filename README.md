# LinkMyPhone

An independent, unofficial Phone Link client for Linux.

LinkMyPhone aims to bring the Phone Link experience to Linux through interoperability with Microsoft's Phone Link and Link to Windows ecosystem. Clipboard synchronization is the first implemented feature, not the limit of the project's scope.

[Get started](docs/getting-started/installation.md) · [Documentation](docs/README.md) · [Protocol and research](docs/research/README.md)

<picture>
  <source media="(prefers-color-scheme: dark) and (max-width: 640px)" srcset="docs/assets/sync-flow-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="docs/assets/sync-flow-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/sync-flow-dark.svg">
  <img src="docs/assets/sync-flow-light.svg" width="860" alt="Current clipboard feature: two-way text synchronization between Linux and Android through Microsoft's DCG and Hub Relay services.">
</picture>

*The diagram shows the current clipboard feature, not the full scope of the project.*

The current implementation is a reverse-engineered, command-line client supporting bidirectional text, HTML and image clipboard synchronization. A personal Microsoft account and an internet connection are required.

LinkMyPhone is an independent, unofficial project, not affiliated with, endorsed by, or sponsored by Microsoft. Microsoft, Phone Link, and Link to Windows are trademarks of the Microsoft group of companies.

> [!NOTE]
>
> Experimental. Two-way sync was reported working on a Samsung Galaxy S23 with CachyOS/Wayland. Long-running reconnect, wake recovery, and active-session token refresh remain open. [Validation and limits](docs/research/validation.md).

## Try the current clipboard feature

You need:

- Linux, Git, and Go 1.23 or newer.
- `wl-clipboard` on Wayland, or `xclip` on X11 (`xsel` is a text-only fallback).
- Optional Python 3 with PyGObject and GTK4 for HTML offers with a plain-text fallback.
- Link to Windows on an Android phone, signed into the same personal Microsoft account.

### 1. Build

```bash
git clone https://github.com/YMGPwcca/linkmyphone.git
cd linkmyphone
go build -o linkmyphone ./cmd/linkmyphone
```

See [installation](docs/getting-started/installation.md) for source access and desktop setup.

### 2. Sign in

```bash
./linkmyphone bootstrap-probe
```

Follow the device-code instructions printed in the terminal.

### 3. Start syncing

> [!WARNING]
>
> New clipboard contents, including secrets, is sent through Microsoft's services. The client does not filter sensitive selections. Do not share `state.json`: it contains refresh credentials and private keys. [Privacy and local state](docs/operations/privacy-and-state.md).

```bash
./linkmyphone feature create --enabled linkmyphone.clipboard
./linkmyphone run
```

Wait for `Modular runtime is running`, then copy fresh, non-sensitive text and try pasting on the other device. The existing clipboard is not published at startup by default. Press Ctrl+C to stop.

<details>
<summary>Already enrolled, or choosing a specific phone?</summary>

Enable an existing clipboard feature record instead of creating it again. If several Android peers are linked, select one explicitly:

```bash
./linkmyphone run --target "PHONE_NAME"
```

The [first-run guide](docs/getting-started/first-run.md) covers resuming a profile, target selection, and checking both clipboard directions.

</details>

## Current support and broader scope

LinkMyPhone aims to provide broader Phone Link integration on Linux. The matrix below details what is working today, what is partially implemented in the codebase, and what remains in the broader project scope:

| Feature / Area | Status | Current implementation details |
| :--- | :---: | :--- |
| **Plain-text clipboard** | Supported | Bidirectional text synchronization, clipboard clears, and echo suppression. |
| **Wayland & X11 clipboard** | Supported | MIME polling with `wl-clipboard` or `xclip`; `xsel` supports text only. |
| **Microsoft sign-in & DCG** | Supported | Microsoft OAuth device-code login, DCG enrollment, key-pair creation, and identity resume. |
| **Device discovery & wake** | Supported | Linked Android peer discovery and EC-signed wake push via Microsoft Hub Relay. |
| **Relay & PLATFORM transport** | Supported | WebSocket SignalR Hub Relay and PLATFORM session validation handshake. |
| **Feature lifecycle & CLI** | Supported | Live enable, disable, configure, or remove feature modules via CLI and Unix socket control plane. |
| **Background daemon** | Supported | Per-user `systemd` unit (`linkmyphone.service`) tied to the graphical session. |
| **Rich text & HTML clipboard** | Implemented | HTML fragments in both directions; optional GTK4 provider also offers plain text. Live phone validation remains required. |
| **Image clipboard** | Implemented | PNG transfer, JPEG/GIF/BMP input normalization, outbound resizing to 1 MiB; incoming dimensions preserved, and existing DCG fragmentation. Live phone validation remains required. |
| **Session recovery & refresh** | Partial | Long-running relay reconnect, sleep/wake recovery, and active-session token refresh remain open. |
| **Notifications** | Not implemented | Push notification forwarding, dismissal synchronization, and inline quick-replies. |
| **Messages (SMS / RCS)** | Not implemented | Reading SMS/RCS conversations, sending text messages, and MMS media attachments. |
| **Calls & Audio** | Not implemented | Incoming/outgoing call notifications, dialer, and in-call Bluetooth/relay audio routing. |
| **Photos & Media** | Not implemented | Android camera roll browsing, photo synchronization, and media caching. |
| **File transfer & Sharing** | Not implemented | Drag-and-drop file sharing between Linux desktop and Android. |
| **Screen mirroring & Apps** | Not implemented | Android screen streaming and individual app streaming. |
| **Graphical pairing UI** | Not implemented | Initial setup is CLI-based (`linkmyphone bootstrap-probe`); no native GUI pairing wizard. |

> [!NOTE]
> Unimplemented features reflect the broader project scope and architecture targets. They do not constitute a commitment to a specific delivery schedule or a claim of feature parity with Microsoft's Windows Phone Link client.

## Run in the background

Stop the foreground runtime first, then install the user service:

```bash
./linkmyphone service install
~/.local/bin/linkmyphone service status
```

Custom state paths or phone target? Configure them with the [systemd guide](docs/operations/systemd.md) before starting the service.

## Find your next step

| You want to… | Read |
| --- | --- |
| Set up and use clipboard sync | [First run](docs/getting-started/first-run.md) · [Clipboard behavior](docs/user-guide/clipboard.md) |
| Find a setting or fix a failure | [CLI](docs/reference/cli.md) · [Configuration](docs/reference/configuration.md) · [Troubleshooting](docs/operations/troubleshooting.md) |
| Work on the client | [Architecture](docs/architecture/overview.md) · [Contributing](docs/developer/contributing.md) · [Source map](docs/developer/source-map.md) |
| Follow the reverse engineering | [Findings](docs/research/findings.md) · [Development history](docs/research/history.md) |

Browse the [full documentation index](docs/README.md) for the remaining guides and references.

<details>
<summary>Authentication stages and development checks</summary>

Startup includes Microsoft device-code sign-in, persistent DCG enrollment, linked-device discovery, trust synchronization, peer wake, and session validation. [Authentication and bootstrap](docs/protocol/authentication.md) follows that sequence.

Run the existing tests on Linux:

```bash
go test ./...
go test -race ./...
```

The [testing guide](docs/developer/testing.md) covers prerequisites and which checks prove which behavior. GitHub Actions runs on the clipboard development branch and through manual dispatch. Automated tests do not establish live Microsoft or phone compatibility.

The repository does not track the original decompiled Windows sources or production capture logs. The [research record](docs/research/README.md) identifies the available evidence and its limits.

</details>

## License

Licensed under the [MIT License](LICENSE).
