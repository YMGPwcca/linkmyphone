# Installation

[Documentation index](../README.md)

LinkMyPhone is an independent, unofficial Phone Link client for Linux, written in Go. It aims to bring the Phone Link experience to Linux through interoperability with Microsoft's Phone Link and Link to Windows ecosystem, rather than remain a clipboard-only tool.

The current implementation is command-line based and uses Microsoft's cloud services. It includes bidirectional text, HTML and image clipboard synchronization and an experimental notification module. This guide installs their desktop prerequisites; it does not imply full Phone Link feature parity. See [current support and broader scope](../../README.md#current-support-and-broader-scope).

LinkMyPhone is independent and unofficial, with no Microsoft affiliation, endorsement, or sponsorship. Microsoft, Phone Link, and Link to Windows are trademarks of the Microsoft group of companies.

There is no package repository or release binary. Build the program from a checkout.

## Quick start

1. Check the prerequisites below.
2. Install a clipboard provider for your graphical session.
3. Clone and build the program.
4. Put the binary on your `PATH`.
5. Continue with [first run and enrollment](first-run.md).

## Prerequisites

You need:

- Linux with Go 1.23 or newer. The required Go version is declared in [`go.mod`](../../go.mod).
- Git for cloning the source.
- A Microsoft account that already has the Windows PC and phone linked in Link to Windows / Phone Link.
- A graphical user session. Wayland requires `wl-clipboard`; X11 requires `xclip` or `xsel`.
- Network access to Microsoft identity, DCG, and SignalR services. The program uses Microsoft cloud services for sign-in, enrollment, trust discovery, wake, and relay traffic.

LinkMyPhone does not contain a new-device pairing wizard. After sign-in, it can enroll the Linux process as a DCG device and use the linked devices returned by Microsoft's service. It does not promise support for every Android device.

### Choose a clipboard provider

Install exactly one provider that matches your session, or install more than one if you need fallback selection:

| Session and tools | Provider selected |
| --- | --- |
| `WAYLAND_DISPLAY` is set and both `wl-paste` and `wl-copy` are on `PATH` | `wl-clipboard`; MIME polling for text/HTML/images |
| `DISPLAY` is set and `xclip` is on `PATH` | `xclip`; text, HTML and images |
| `DISPLAY` is set and `xclip` is absent, with `xsel` on `PATH` | `xsel`; plain text only |
| Session variables are missing but a complete provider is on `PATH` | The first complete provider found, preferring `wl-clipboard`, then `xclip`, then `xsel` |

The detector does not install these utilities. A missing provider produces `no supported Linux clipboard backend found`; install `wl-clipboard`, `xclip`, or `xsel` with your distribution's package manager. The program executes these utilities directly, without a shell.

## Build from source

The module path is `github.com/YMGPwcca/linkmyphone`. If the repository URL asks for credentials, use an account with repository access and configure Git authentication. Repository access and Microsoft sign-in are separate.

```bash
git clone https://github.com/YMGPwcca/linkmyphone.git
cd linkmyphone
go build -o linkmyphone ./cmd/linkmyphone
```

Put the resulting binary somewhere on your `PATH`, for example:

```bash
install -Dm755 linkmyphone "$HOME/.local/bin/linkmyphone"
export PATH="$HOME/.local/bin:$PATH"
linkmyphone help
```

The systemd installer can also copy the currently running executable to `~/.local/bin/linkmyphone`; see [the systemd user service guide](../operations/systemd.md). It is optional. Running `linkmyphone run` directly is supported.

## Files and permissions

By default, authentication state is stored at `~/.config/linkmyphone/state.json`. The feature registry is stored separately at `~/.config/linkmyphone/features.json`. The program creates their parent directory with mode `0700` and writes each file with mode `0600`.

Authentication state contains a Microsoft refresh token, DCG and trust private keys and certificates, service tokens, account certificate data, and linked-device trust relationships. Treat it as a credential. Do not commit it, upload it, or attach it to a bug report. See [privacy and state](../operations/privacy-and-state.md).

## Continue with first run

After the binary and a clipboard provider are available, follow [first run and enrollment](first-run.md). The first run uses Microsoft device-code sign-in and creates persistent state before it attempts the later trust and relay stages.

Implementation references: [`auth/state/store.go`](../../auth/state/store.go) defines state paths and file modes; [`clipboard/native.go`](../../clipboard/native.go) defines provider detection; provider behavior is covered by [`clipboard/native_test.go`](../../clipboard/native_test.go).

## HTML dependencies and existing installations

For received HTML to offer both rich formatting and plain text to applications, install Python 3, PyGObject and GTK4. Without them, HTML remains available, but a text-only paste target may not accept it. On Arch/CachyOS:

```bash
sudo pacman -S --needed wl-clipboard gtk4 python-gobject
```

Notification reception additionally needs a desktop session bus and a running `org.freedesktop.Notifications` service. Android actions require native action support; replies use the same Python GI/GTK4 packages. Missing GTK disables reply controls/capability without substituting a fake reply backend.

Fresh installs enroll once as Phone Link (PL) for clipboard and notifications. Existing CrossDevice (WEA) states remain unchanged; select an existing PL state or enroll a new PL identity on another path before enabling full notification push. See [one-login setup and safe cutover](first-run.md#existing-crossdevice-and-trial-phone-link-enrollments) and [validation limits](../research/validation.md#notification-validation-2026-10-04).


Existing `phonelink-linux` installations can reuse their enrollment through `--state "$HOME/.config/phonelink-linux/state.json"`. Stop `phonelink-linux.service` before starting a test binary, and invoke the newly built executable explicitly; running the old installed command does not test the new code. See [clipboard behavior](../user-guide/clipboard.md) for supported image codecs and limits.
