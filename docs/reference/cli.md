# Command-line reference

[Documentation index](../README.md)

The executable is `linkmyphone`. Each command uses Go's standard flag parser: put flags before positional IDs, as in `feature get --state PATH ID` and `feature update --enabled true ID`.

Run `linkmyphone help` for the command list. Invalid arguments and operational failures return a non-zero status. Probe output omits secrets and clipboard contents.

## Choose a command

| Need | Command | What it does |
| --- | --- | --- |
| Sign in or resume enrollment | `bootstrap-probe` | Uses Microsoft device code, enrolls a Linux DCG identity on first run, or resumes existing state; refreshes linked trust and connects the account-level SignalR relay. |
| Check peer presence | `peer-probe` | Resumes state, selects a linked peer, sends signed wake when needed, and waits for Hub Relay presence. |
| Validate a session | `session-probe` | Resumes state and peer presence, then sends PLATFORM `/SessionValidation`; optionally runs the clipboard ContextSource probe. |
| Manage desired features | `feature` | Reads and mutates the persistent modular feature registry. Live mutations are sent to a running runtime when its control socket is ready. |
| Run enabled features | `run` | Opens one Phone Link host session and starts every enabled feature in the registry. |
| Manage systemd | `service` | Installs and manages the optional per-user systemd unit. |
| Run clipboard without the registry | `clipboard-sync` | Compatibility alias that starts `linkmyphone.clipboard` without changing `features.json`. |

## Authentication and probes

### `bootstrap-probe`

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | Platform user config plus `linkmyphone/state.json`, normally `~/.config/linkmyphone/state.json` | Persistent enrollment, token, key, certificate, and trust state path. |
| `--profile PROFILE` | Existing state's profile, otherwise `phonelink` | First enrollment only: `phonelink` (PL, clipboard + notifications) or `crossdevice` (legacy WEA, clipboard only). A mismatched existing profile is refused; a new PL enrollment can use the default state path. |
| `--app-version VERSION` | `1.26072.116.0` | Compatibility app version sent in DCG metadata. |
| `--ring NAME` | `Public` | Compatibility ring metadata. |
| `--os-version VERSION` | `10.0.26100` | Windows-compatible OS version metadata. |
| `--display-name NAME` | Local hostname, or `linkmyphone` if unavailable | Display name used when enrolling the Linux device. |
| `--signalr-timeout DURATION` | `10s` | Time to wait for SignalR `OnConnected`. |

`peer-probe` accepts `--state`, `--app-version`, `--ring`, `--os-version`, and `--signalr-timeout` with the same defaults and effects.

| `peer-probe` flag | Default | Effect |
| --- | --- | --- |
| `--target SELECTOR` | Empty | Linked peer ID, name, model, or unambiguous partial selector. Empty selects the sole linked Android device. |
| `--wake-timeout DURATION` | `45s` | Time to wait for target presence after a wake request. |
| `--wake-ttl DURATION` | `60s` | Dispatcher wake time-to-live. |

`session-probe` accepts all `peer-probe` flags, plus:

| Flag | Default | Effect |
| --- | --- | --- |
| `--request-timeout DURATION` | `10s` | Time to wait for the `/SessionValidation` response. |
| `--context-probe` | `false` | Sends the source-confirmed clipboard publication shape after SessionValidation. |
| `--context-timeout DURATION` | `8s` | Time to observe the peer's PLATFORM reaction. |
| `--context-text TEXT` | Empty | Text to return if the peer requests clipboard CONTENT. Requires `--context-probe`; omitted text makes the probe decline CONTENT rather than send clipboard text. |

All duration and TTL values must be positive. Probe flags must precede any command arguments; these probes do not accept positional arguments.

## Feature registry

The default registry is `~/.config/linkmyphone/features.json` under the platform user config directory. For `feature` commands, `--state` selects this registry, not the authentication state used by `run` and probes.

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | Platform user config plus `linkmyphone/features.json` | Feature registry path. |

Command forms:

```text
linkmyphone feature list [--state PATH]
linkmyphone feature get [--state PATH] ID
linkmyphone feature create [--state PATH] [--enabled] [--config JSON] ID
linkmyphone feature update [--state PATH] [--enabled true|false] [--config JSON] ID
linkmyphone feature delete [--state PATH] ID
linkmyphone feature enable [--state PATH] ID
linkmyphone feature disable [--state PATH] ID
```

| Subcommand | Behavior |
| --- | --- |
| `list` | Reports built-in definitions and installed records. Against a ready runtime it reports live state, module state, and epoch; without a live socket it reports desired state from the file. |
| `get` | Prints JSON containing the known manifest, installed record, and, when live, the runtime snapshot. |
| `create` | Installs one known feature. `--enabled` defaults to false. `--config JSON` supplies initial configuration and is checked by the feature's Go validator; built-in validators reject explicit null properties as well as unknown fields, wrong types and invalid ranges. |
| `update` | Requires `--enabled`, `--config`, or both. `--enabled` accepts `true`, `false`, `1`, `0`, `yes`, `no`, `on`, and `off`. `--config` replaces the whole JSON configuration object. A live update stops and restarts the module when needed. |
| `enable` / `disable` | Change desired enabled state. `enable` rejects an unavailable feature. |
| `delete` | Removes the record and, when live, stops and removes its module instance. It does not close the shared Phone Link session. |

A live command uses the Unix control socket. If the socket is absent, refused, or not a socket, the command falls back to offline registry file behavior. If the runtime is starting, it returns `runtime is starting; retry the feature command` instead of making an offline edit. During recovery it returns `runtime is recovering; retry the feature command` through the same socket. See [configuration and state](configuration.md).

## Modular runtime: `run`

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | `~/.config/linkmyphone/state.json` | Authentication and enrollment state. |
| `--features-state PATH` | `~/.config/linkmyphone/features.json` | Desired feature registry. |
| `--app-version VERSION` | `1.26072.116.0` | CrossDevice app metadata. |
| `--ring NAME` | `Public` | CrossDevice ring metadata. |
| `--os-version VERSION` | `10.0.26100` | Windows-compatible OS metadata. |
| `--target SELECTOR` | Empty | Linked peer selector; empty requires exactly one linked Android peer. |
| `--signalr-timeout DURATION` | `10s` | SignalR `OnConnected` wait. |
| `--wake-timeout DURATION` | `45s` | Peer presence wait after wake. |
| `--wake-ttl DURATION` | `60s` | Dispatcher wake time-to-live. |
| `--request-timeout DURATION` | `10s` | Time to wait for the host's `/SessionValidation` response. Clipboard requests use the module's `request_timeout_ms` configuration. |
| `--reconnect-min-delay DURATION` | `1s` | Initial recovery backoff base; equal jitter waits between half and all of it. |
| `--reconnect-max-delay DURATION` | `1m` | Backoff base cap. Retry-After can impose a longer wait. |
| `--session-open-timeout DURATION` | `2m` | Deadline for one complete auth/trust/relay/wake/SessionValidation attempt. |
| `--refresh-margin DURATION` | `2m` | Renew before earliest token expiry; capped at 20% of token lifetime. |

Timeout, TTL, and recovery values must be positive; maximum backoff must be at least the minimum. At startup, `run` reserves the feature store's control socket, opens the host, and starts enabled modules before reporting readiness. Both `run` and `clipboard-sync` use [automatic recovery](../operations/session-recovery.md). Run one runtime per feature store.

## Compatibility clipboard command

`clipboard-sync` accepts all host flags from `run` except `--features-state`, plus:

| Flag | Default | Effect |
| --- | --- | --- |
| `--poll-interval DURATION` | `500ms` | MIME observation interval on rich providers; fallback polling interval on text-only providers. It must be between `50ms` and `60s`. |
| `--publish-initial` | `false` | Publishes the current Linux clipboard on each module start, including after recovery. |

The inherited host flags are `--state`, `--app-version`, `--ring`, `--os-version`, `--target`, `--signalr-timeout`, `--wake-timeout`, `--wake-ttl`, `--request-timeout`, `--reconnect-min-delay`, `--reconnect-max-delay`, `--session-open-timeout`, and `--refresh-margin`, with the `run` defaults above.

Unlike `run`, `clipboard-sync` also copies `--request-timeout` into its in-memory clipboard configuration. That value must satisfy the module's 100 through 120000 millisecond range.

## Systemd user-service commands

```text
linkmyphone service install [--enable=true] [--start=true]
linkmyphone service uninstall [--keep-binary]
linkmyphone service start|stop|restart
linkmyphone service enable|disable|status
linkmyphone service logs [--follow] [--lines N]
linkmyphone service import-environment
```

| Command or option | Behavior |
| --- | --- |
| `install` | Defaults to `--enable=true` and `--start=true`. `--enable=false` skips enabling; it does not disable an already enabled unit. `--start=false` skips starting or restarting; it does not stop an existing process. |
| `uninstall` | Removes the unit and installed binary by default. `--keep-binary` keeps `~/.local/bin/linkmyphone`. Authentication state and feature registry files are not removed. |
| `start`, `stop`, `restart`, `enable`, `disable`, `status` | Take no additional arguments. `start` and `restart` import the current graphical environment and clear failed/start-limit state before invoking systemd. |
| `logs` | `--follow` follows new user-journal entries. `--lines N` defaults to `100` and must be non-negative. |
| `import-environment` | Imports supported graphical and XDG variables into the systemd user manager without starting the unit. |

The [systemd user service guide](../operations/systemd.md) describes unit readiness, environment import, lifecycle, and recovery.

The command implementations are [`cmd/linkmyphone/main.go`](../../cmd/linkmyphone/main.go), [`cmd/linkmyphone/feature.go`](../../cmd/linkmyphone/feature.go), [`cmd/linkmyphone/runtime_run.go`](../../cmd/linkmyphone/runtime_run.go), and [`cmd/linkmyphone/service.go`](../../cmd/linkmyphone/service.go). CLI behavior is exercised by [`cmd/linkmyphone/feature_test.go`](../../cmd/linkmyphone/feature_test.go), [`cmd/linkmyphone/runtime_control_test.go`](../../cmd/linkmyphone/runtime_control_test.go), and [`cmd/linkmyphone/service_test.go`](../../cmd/linkmyphone/service_test.go).
