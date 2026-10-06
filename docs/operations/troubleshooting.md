# Troubleshooting

[Documentation index](../README.md)

Check the first failing stage in this order: **state → target → relay and session → clipboard module**. A clipboard error may be caused by an earlier failure. Use the symptom table below to find the matching action.


## Find the failing stage

| Symptom or message | Likely stage | Action |
| --- | --- | --- |
| `no persisted enrollment found; run bootstrap-probe first` | Local authentication state | Run `bootstrap-probe` with the same `--state` path that `run` will use. |
| `state: unsupported version` or state decode error | State loading | Keep a backup, inspect the path and version, and do not delete it to force a second identity. A malformed state needs recovery from a known-good backup or a deliberate fresh profile. |
| Device-code URL or code never completes | Microsoft sign-in | Complete sign-in for the Microsoft account that owns the existing Link to Windows relationship. Check network and browser access. Do not paste tokens into a report. |
| `resume authentication` or `Microsoft login` failure | Token refresh or DCG sign-in | Temporary network/service failures retry with backoff. Revoked refresh credentials (`invalid_grant`) and permanent authorization errors stop recovery; inspect account access and keep the existing state. A service restart cannot restore revoked credentials. |
| `no linked peers returned by DeviceInfoList` | Account/device discovery | Confirm the Windows PC and phone are already linked in Link to Windows / Phone Link under this account. Linux has no new-pairing wizard. |
| `no linked Android device found` | Default target selection | The default needs a linked Android device. An explicit `--target` can select another returned linked peer, but acceptance of that selector does not establish clipboard compatibility. |
| More than one linked Android device is reported | Default target selection | Supply `--target` with the peer ID, name, model, or an unambiguous partial match. |
| `target ... is ambiguous` or `matches multiple linked devices` | Target selection | Make the selector more specific. The program does not choose arbitrarily. |
| `SignalR bootstrap`, `no usable SignalR shard`, or `OnConnected` timeout | Cloud relay | Check network access to Microsoft's DCG and SignalR services. Increase `--signalr-timeout` only when the environment is slow; it does not repair a rejected cloud connection. |
| `peer presence` or wake timeout | Peer wake | Confirm the target is linked and reachable. Try `peer-probe --target ... --wake-timeout 60s`. The runtime retries wake/re-presence automatically; finite probes still stop on failure. |
| `SessionValidation` timeout or rejection | PLATFORM session | Run `session-probe` after `peer-probe` and inspect the rejection reason. This stage requires the target to be present on Hub Relay. |
| `clipboard: no supported Linux clipboard backend found` | Native clipboard startup | Install `wl-clipboard`, `xclip`, or `xsel`; ensure the selected graphical session's provider is on `PATH`. |
| `wl-paste` or `wl-copy` connection error | Wayland environment | Check `WAYLAND_DISPLAY`, the user manager environment, and access to the current compositor. For a service, run `service import-environment` and restart. |
| `xclip` or `xsel` connection error | X11 environment | Check `DISPLAY`, `XAUTHORITY`, and the X11 provider from the same user session that runs the process. |
| `clipboard: content too large` | Typed content budget | Text/HTML must stay below 131072 UTF-16 units. Phone image input is capped at 16 MiB, desktop PNG at 128 MiB and decoded dimensions at 33554432 pixels. Only outbound PNG must fit 1 MiB. |
| `clipboard: unsupported content` | MIME or codec | Supported images are PNG/JPEG/GIF/BMP. File selections, HEIC/AVIF/TIFF/WebP and malformed images are skipped; a later supported copy can resume sync. |
| HTML pastes into a rich editor but not a text-only app | Native HTML offer | Install Python 3, PyGObject and GTK4 for the dual HTML/plain-text provider. `xsel` supports only plain text. |
| Received phone image is smaller than the original file | Sender or old binary | Android may scale before sending. Current Linux conversion preserves received dimensions; rebuild and run the new binary if Linux still shrinks more than Windows. |
| `native clipboard watch unavailable; using polling fallback` | Watcher | The module continues with `poll_interval_ms`. If polling fails, inspect the following error and provider logs. |
| Module is not `Ready` | Feature lifecycle | Run `feature get linkmyphone.clipboard` and inspect the runtime snapshot. A disabled record, invalid config, unavailable provider, or failed host stage prevents readiness. |
| `runtime is starting; retry the feature command` | Control plane startup | Wait for systemd readiness or the interactive runtime's ready message, then retry. The command intentionally does not make an offline edit during startup. |
| `runtime is recovering; retry the feature command` | Session handoff | Wait for PLATFORM and module readiness, then retry. The socket remains reserved and no offline edit is made. See [session recovery](session-recovery.md). |
| Feature command reports offline desired state | Control socket unavailable | Confirm that `run` is active and that the command uses the same `--state` feature-store path. Offline commands edit the registry only; they do not prove the module can start. |
| `runtime already running` or a socket conflict | Two runtimes | Stop the interactive runtime before starting systemd, or stop the service before running interactively. One feature store has one owning runtime. |

## Inspect an interactive run

Run in the foreground to see host stages and module events:

```bash
linkmyphone run
```

The runtime prints the selected target, host stages, module state, live capabilities, and control socket path, but not clipboard contents. Stop with Ctrl+C and check the shutdown messages.

Use a separate terminal for live feature state:

```bash
linkmyphone feature list
linkmyphone feature get linkmyphone.clipboard
```

If you use a non-default registry, pass the same `--state` to both feature commands and `--features-state` to `run`.

## Inspect a systemd run

```bash
~/.local/bin/linkmyphone service status
~/.local/bin/linkmyphone service logs --lines 250
~/.local/bin/linkmyphone service logs --follow
```

Check the first error before restarting repeatedly. The unit allows five starts in 60 seconds. Service helpers reset failed/start-limit state before `install`, `start`, and `restart`. When using `systemctl` directly, recover with:

```bash
systemctl --user reset-failed linkmyphone.service
~/.local/bin/linkmyphone service import-environment
~/.local/bin/linkmyphone service start
```

If startup remains `activating`, inspect the host stages. With `Type=notify`, the unit waits until the control plane and enabled modules are ready. Feature commands return a retry response during startup.

An error beginning `notifications module: initialize desktop` identifies notification-service initialization. Check that the session bus and desktop notification service are available. Startup uses the feature's `request_timeout_ms` and honors earlier caller cancellation or deadlines; increasing it does not repair a missing service. See [notification configuration](../reference/configuration.md#notification-configuration).

## Report a bug safely

Include the smallest reproducer that identifies the failure stage, the command and flags, operating system, desktop session type, selected provider (`wl-clipboard`, `xclip`, or `xsel`), and exact non-secret error text. For code-level issues, include relevant source paths and versions.

For service problems, attach a redacted excerpt from `journalctl --user -u linkmyphone.service`. Remove:

- the complete `state.json` and `features.json` files unless a maintainer explicitly provides a secure transfer method;
- Microsoft refresh tokens, access tokens, DCG service tokens, private keys, certificates and account certificate data;
- device IDs, peer names, account identifiers, hostnames, usernames, custom paths, and environment values when they identify you;
- clipboard text. Logs should report byte counts and shortened correlation IDs, but redact any copied text if another tool included it.

Never run a report command that prints the state file. For a state-parser bug, share only a separate copy with every credential and identifying value removed, and verify that it cannot authenticate.
