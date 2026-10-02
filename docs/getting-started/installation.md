# Installation

[Documentation index](../README.md)

Phone Link Linux is a Go program for Linux that connects to Microsoft Phone Link / Link to Windows through Microsoft's cloud services. Its current built-in feature is bidirectional text clipboard synchronization.

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

Phone Link Linux does not contain a new-device pairing wizard. After sign-in, it can enroll the Linux process as a DCG device and use the linked devices returned by Microsoft's service. It does not promise support for every Android device.

### Choose a clipboard provider

Install exactly one provider that matches your session, or install more than one if you need fallback selection:

| Session and tools | Provider selected |
| --- | --- |
| `WAYLAND_DISPLAY` is set and both `wl-paste` and `wl-copy` are on `PATH` | `wl-clipboard` with event watching |
| `DISPLAY` is set and `xclip` is on `PATH` | `xclip` |
| `DISPLAY` is set and `xclip` is absent, with `xsel` on `PATH` | `xsel` |
| Session variables are missing but a complete provider is on `PATH` | The first complete provider found, preferring `wl-clipboard`, then `xclip`, then `xsel` |

The detector does not install these utilities. A missing provider produces `no supported Linux clipboard backend found`; install `wl-clipboard`, `xclip`, or `xsel` with your distribution's package manager. The program executes these utilities directly, without a shell.

## Build from source

If GitHub returns 404 or asks for credentials, use an account with access to the repository and configure Git authentication. Repository access and Microsoft sign-in are separate.

```bash
git clone https://github.com/YMGPwcca/phonelink-linux.git
cd phonelink-linux
go build -o phonelink-linux ./cmd/phonelink-linux
```

Put the resulting binary somewhere on your `PATH`, for example:

```bash
install -Dm755 phonelink-linux "$HOME/.local/bin/phonelink-linux"
export PATH="$HOME/.local/bin:$PATH"
phonelink-linux help
```

The systemd installer can also copy the currently running executable to `~/.local/bin/phonelink-linux`; see [the systemd user service guide](../operations/systemd.md). It is optional. Running `phonelink-linux run` directly is supported.

## Files and permissions

By default, authentication state is stored at `~/.config/phonelink-linux/state.json`. The feature registry is stored separately at `~/.config/phonelink-linux/features.json`. The program creates their parent directory with mode `0700` and writes each file with mode `0600`.

Authentication state contains a Microsoft refresh token, DCG and trust private keys and certificates, service tokens, account certificate data, and linked-device trust relationships. Treat it as a credential. Do not commit it, upload it, or attach it to a bug report. See [privacy and state](../operations/privacy-and-state.md).

## Continue with first run

After the binary and a clipboard provider are available, follow [first run and enrollment](first-run.md). The first run uses Microsoft device-code sign-in and creates persistent state before it attempts the later trust and relay stages.

Implementation references: [`auth/state/store.go`](../../auth/state/store.go) defines state paths and file modes; [`clipboard/native.go`](../../clipboard/native.go) defines provider detection; provider behavior is covered by [`clipboard/native_test.go`](../../clipboard/native_test.go).
