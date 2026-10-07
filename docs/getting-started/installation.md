# Installation

[Documentation index](../README.md)

There is no package repository or release binary; build LinkMyPhone from source. This guide covers Linux requirements, clipboard providers and first-run setup. See [current support](../../README.md#current-support) for implemented features.



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

LinkMyPhone has no new-device pairing wizard. After sign-in, it enrolls the Linux process as a DCG device and uses linked devices returned by Microsoft's service. Support is not guaranteed for every Android device.

### Choose a clipboard provider

Install a provider for your session. You can install additional providers for fallback selection:

| Session and tools | Provider selected |
| --- | --- |
| `WAYLAND_DISPLAY` is set and both `wl-paste` and `wl-copy` are on `PATH` | `wl-clipboard`; MIME polling for text/HTML/images |
| `DISPLAY` is set and `xclip` is on `PATH` | `xclip`; text, HTML and images |
| `DISPLAY` is set and `xclip` is absent, with `xsel` on `PATH` | `xsel`; plain text only |
| Session variables are missing but a complete provider is on `PATH` | The first complete provider found, preferring `wl-clipboard`, then `xclip`, then `xsel` |

Install the utilities with your distribution's package manager. If none is available, startup reports `no supported Linux clipboard backend found`.

## Build from source


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

The optional [systemd installer](../operations/systemd.md) can copy the running executable to `~/.local/bin/linkmyphone`. You can also run `linkmyphone run` directly.

## Files and permissions

By default, authentication state is stored at `~/.config/linkmyphone/state.json`. The feature registry is stored separately at `~/.config/linkmyphone/features.json`. The program creates their parent directory with mode `0700` and writes each file with mode `0600`.

Authentication state contains a Microsoft refresh token, DCG and trust private keys and certificates, service tokens, account certificate data, and linked-device trust relationships. Treat it as a credential. Do not commit it, upload it, or attach it to a bug report. See [privacy and state](../operations/privacy-and-state.md).


## HTML and notification dependencies

For received HTML to offer both rich formatting and plain text to applications, install Python 3, PyGObject and GTK4. Without them, HTML remains available, but a text-only paste target may not accept it. On Arch/CachyOS:

```bash
sudo pacman -S --needed wl-clipboard gtk4 python-gobject
```

Phone notifications need notification access for Link to Windows and a desktop service implementing `org.freedesktop.Notifications`. App buttons need action support from that service. Replies also need Python GI/GTK4; without it, notifications still arrive but reply buttons are unavailable.

One fresh Phone Link (PL) enrollment supports clipboard and notifications. Existing CrossDevice (WEA) state remains unchanged. See the [first-run migration guidance](first-run.md#existing-crossdevice-and-trial-phone-link-enrollments) before adding notifications to an existing installation.

To reuse an existing `phonelink-linux` enrollment, pass `--state "$HOME/.config/phonelink-linux/state.json"` and stop `phonelink-linux.service` first. Run the newly built executable explicitly; invoking the old installed command does not test it.
