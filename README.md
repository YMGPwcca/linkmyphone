# LinkMyPhone

Copy text between your Linux desktop and Android phone.

[Get started](docs/getting-started/installation.md) · [Documentation](docs/README.md) · [Protocol and research](docs/research/README.md)

<picture>
  <source media="(prefers-color-scheme: dark) and (max-width: 640px)" srcset="docs/assets/sync-flow-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="docs/assets/sync-flow-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/sync-flow-dark.svg">
  <img src="docs/assets/sync-flow-light.svg" width="860" alt="Two-way text clipboard synchronization between Linux and Android through Microsoft's DCG and Hub Relay services.">
</picture>

A reverse-engineered, command-line client. A personal Microsoft account and an internet connection are required.

LinkMyPhone is an independent, unofficial project, not affiliated with, endorsed by, or sponsored by Microsoft. Microsoft, Phone Link, and Link to Windows are trademarks of the Microsoft group of companies.

> [!NOTE]
>
> Experimental. Two-way sync was reported working on a Samsung Galaxy S23 with CachyOS/Wayland. Long-running reconnect, wake recovery, and active-session token refresh remain open. [Validation and limits](docs/research/validation.md).

## Get it running

You need:

- Linux, Git, and Go 1.23 or newer.
- `wl-clipboard` on Wayland, or `xclip` / `xsel` on X11.
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
> New clipboard text, including secrets, is sent through Microsoft's services. The client does not filter sensitive selections. Do not share `state.json`: it contains refresh credentials and private keys. [Privacy and local state](docs/operations/privacy-and-state.md).

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

## What is supported

| Area            | Current behavior                                         |
| --------------- | -------------------------------------------------------- |
| Clipboard       | Plain text in both directions, including clears.         |
| Linux desktop   | Wayland watching; polling for X11 and watch failures.    |
| Feature control | Live enable, disable, configuration update, and removal. |
| Background use  | A systemd user service tied to the graphical session.    |

Image and HTML codecs exist, but the clipboard feature syncs text only. Notifications, calls, photos, file transfer, and a graphical pairing interface are not implemented.

## Run in the background

Stop the foreground runtime first, then install the user service:

```bash
./linkmyphone service install
~/.local/bin/linkmyphone service status
```

Custom state paths or phone target? Configure them with the [systemd guide](docs/operations/systemd.md) before starting the service.

Upgrading from Phone Link Linux? There is **no automatic migration**. Stop and disable the old daemon before moving private state or starting LinkMyPhone; follow [state migration](docs/reference/configuration.md#migrate-from-phone-link-linux) and [service migration](docs/operations/systemd.md#migrate-the-old-service).

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

The [testing guide](docs/developer/testing.md) covers prerequisites and which checks prove which behavior. GitHub Actions runs only when its workflow is dispatched manually. Automated tests do not establish live Microsoft or phone compatibility.

The repository does not track the original decompiled Windows sources or production capture logs. The [research record](docs/research/README.md) identifies the available evidence and its limits.

</details>
