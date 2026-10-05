# LinkMyPhone

LinkMyPhone is an independent, unofficial Linux client for Microsoft's Phone Link. It connects an Android phone to a Linux desktop using Microsoft's services.

Today, it syncs clipboard text, HTML and images and forwards Android notifications to Linux. The goal is broader Phone Link integration; [current support](#current-support) lists what is implemented and what is not.

[Get started](docs/getting-started/installation.md) · [Documentation](docs/README.md) · [Contributing](docs/developer/contributing.md)

<picture>
  <source media="(prefers-color-scheme: dark) and (max-width: 640px)" srcset="docs/assets/sync-flow-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="docs/assets/sync-flow-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/sync-flow-dark.svg">
  <img src="docs/assets/sync-flow-light.svg" width="860" alt="Two-way text, HTML and image clipboard synchronization between Linux and Android through Microsoft's DCG and Hub Relay services.">
</picture>

*Current clipboard sync path.*

## Current support

Clipboard sync works in both directions for text, HTML and images. Incoming JPEG, GIF and BMP images are converted to PNG; outbound images are limited to 1 MiB. Wayland and X11 are supported; `xsel` supports text only.

Android notifications appear on the desktop. You can dismiss them, use app actions and reply when the app supports replies. A Phone Link (PL) sign-in can support both notifications and clipboard sync; existing CrossDevice (WEA) profiles remain clipboard-only.

A per-user `systemd` service is available. Recovery after network loss and suspend has been tested on a Samsung Galaxy S23 with CachyOS/Wayland. Other phones, token expiry and extended runs remain untested. See [validation results](docs/research/validation.md).

Phone Link capabilities not yet implemented:

- **Messages:** reading SMS/RCS conversations, sending text messages and MMS media attachments.
- **Calls and audio:** incoming and outgoing call notifications, a dialer, and in-call Bluetooth/relay audio routing.
- **Photos and media:** Android camera-roll browsing, photo synchronization and media caching.
- **File transfer and sharing:** drag-and-drop file sharing between a Linux desktop and Android.
- **Screen mirroring and apps:** streaming the phone screen or individual apps.
- **Graphical pairing:** setup currently uses the CLI.

This project is not affiliated with, endorsed by or sponsored by Microsoft. Microsoft, Phone Link and Link to Windows are trademarks of the Microsoft group of companies.

## Quick start

You need:

- Linux, Git and Go 1.23 or newer.
- `wl-clipboard` on Wayland, or `xclip` on X11. `xsel` is a text-only fallback.
- A desktop notification service and notification access for Link to Windows on the phone.
- Link to Windows on an Android phone, signed in to the same personal Microsoft account.
- Optional Python 3, PyGObject and GTK4 for rich HTML clipboard offers and notification replies; without them, HTML falls back to plain text.

### Build

```bash
git clone https://github.com/YMGPwcca/linkmyphone.git
cd linkmyphone
go build -o linkmyphone ./cmd/linkmyphone
```

See [installation](docs/getting-started/installation.md) for setup details.

### Sign in

```bash
./linkmyphone bootstrap-probe
```

Follow the device-code instructions in the terminal.

### Start syncing

Clipboard content is sent through Microsoft's services while sync is enabled. Avoid copying secrets. Notification dismissal, app actions and replies can affect the phone. Keep `state.json` private; it contains refresh credentials and private keys. See [privacy and local state](docs/operations/privacy-and-state.md).

```bash
./linkmyphone feature create --enabled linkmyphone.clipboard
./linkmyphone feature create --enabled linkmyphone.notifications
./linkmyphone run
```

Wait for `Modular runtime is running`, then copy non-sensitive text, formatted HTML or an image and paste it on the other device. New phone notifications appear on the desktop; existing notifications do not raise startup alerts by default. The existing clipboard is not published at startup by default. Press Ctrl+C to stop.

<details>
<summary>Already enrolled or selecting a phone</summary>

Enable existing feature records rather than creating them again. If several Android peers are linked, select one explicitly:

```bash
./linkmyphone run --target "PHONE_NAME"
```

See the [first-run guide](docs/getting-started/first-run.md) for profile recovery, phone selection and setup.

</details>

## Run in the background

Stop the foreground runtime, then install the user service:

```bash
./linkmyphone service install
~/.local/bin/linkmyphone service status
```

For a custom state path or phone target, see the [systemd guide](docs/operations/systemd.md) before starting the service.

## Documentation

| Topic | Guide |
| --- | --- |
| Set up and use clipboard sync | [First run](docs/getting-started/first-run.md) · [Clipboard behavior](docs/user-guide/clipboard.md) |
| Find a command, setting or fix | [CLI](docs/reference/cli.md) · [Configuration](docs/reference/configuration.md) · [Troubleshooting](docs/operations/troubleshooting.md) |
| Understand the client | [Architecture](docs/architecture/overview.md) · [Source map](docs/developer/source-map.md) |
| Contribute or test | [Contributing](docs/developer/contributing.md) · [Testing](docs/developer/testing.md) |
| Read protocol and research notes | [Protocol references](docs/README.md#protocol-and-research) · [Research index](docs/research/README.md) |

For setup paths, operations and references, browse the [documentation index](docs/README.md).

## Development

Run the test suite on Linux:

```bash
go test ./...
go test -race ./...
```

See the [testing guide](docs/developer/testing.md) for prerequisites and limits. Automated tests do not establish live Microsoft-service or phone compatibility.

## License

Licensed under the [MIT License](LICENSE).
