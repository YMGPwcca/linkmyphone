# Command-line reference

[Documentation index](../README.md)

The executable is `phonelink-linux`. Go's standard flag parser is used for each command, so put flags before positional IDs, for example `feature get --state PATH ID` and `feature update --enabled true ID`.

Run `phonelink-linux help` for the top-level list. Commands return a non-zero status on invalid arguments or an operational failure. Secrets and clipboard text are intentionally omitted from probe output.

## Choose a command

| Need | Command | What it does |
| --- | --- | --- |
| Sign in or resume enrollment | `bootstrap-probe` | Uses Microsoft device code, enrolls a Linux DCG identity on first run, or resumes existing state; refreshes linked trust and connects the account-level SignalR relay. |
| Check peer presence | `peer-probe` | Resumes state, selects a linked peer, sends signed wake when needed, and waits for Hub Relay presence. |
| Validate a session | `session-probe` | Resumes state and peer presence, then sends PLATFORM `/SessionValidation`; optionally runs the clipboard ContextSource probe. |
| Manage desired features | `feature` | Reads and mutates the persistent modular feature registry. Live mutations are sent to a running runtime when its control socket is ready. |
| Run enabled features | `run` | Opens one Phone Link host session and starts every enabled feature in the registry. |
| Manage systemd | `service` | Installs and manages the optional per-user systemd unit. |
| Run clipboard without the registry | `clipboard-sync` | Compatibility alias that starts `phonelink.clipboard` without changing `features.json`. |

## Authentication and probes

### `bootstrap-probe`

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | Platform user config plus `phonelink-linux/state.json`, normally `~/.config/phonelink-linux/state.json` | Persistent enrollment, token, key, certificate, and trust state path. |
| `--app-version VERSION` | `1.26072.116.0` | CrossDevice app version sent in DCG metadata. |
| `--ring NAME` | `Public` | CrossDevice ring metadata. |
| `--os-version VERSION` | `10.0.26100` | Windows-compatible OS version metadata. |
| `--display-name NAME` | Local hostname, or `phonelink-linux` if unavailable | Display name used when enrolling the Linux device. |
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

The default feature registry path is `~/.config/phonelink-linux/features.json` through the platform user config directory. This `--state` is separate from the authentication state path used by `run` and the probes.

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | Platform user config plus `phonelink-linux/features.json` | Feature registry path. |

Use these forms:

```text
phonelink-linux feature list [--state PATH]
phonelink-linux feature get [--state PATH] ID
phonelink-linux feature create [--state PATH] [--enabled] [--config JSON] ID
phonelink-linux feature update [--state PATH] [--enabled true|false] [--config JSON] ID
phonelink-linux feature delete [--state PATH] ID
phonelink-linux feature enable [--state PATH] ID
phonelink-linux feature disable [--state PATH] ID
```

| Subcommand | Behavior |
| --- | --- |
| `list` | Reports built-in definitions and installed records. Against a ready runtime it reports live state, module state, and epoch; without a live socket it reports desired state from the file. |
| `get` | Prints JSON containing the known manifest, installed record, and, when live, the runtime snapshot. |
| `create` | Installs one known feature. `--enabled` defaults to false. `--config JSON` supplies initial configuration and is checked by the feature's Go validator; the JSON schema documents that contract. |
| `update` | Requires `--enabled`, `--config`, or both. `--enabled` accepts `true`, `false`, `1`, `0`, `yes`, `no`, `on`, and `off`. `--config` replaces the whole JSON configuration object. A live update stops and restarts the module when needed. |
| `enable` / `disable` | Change desired enabled state. `enable` rejects an unavailable feature. |
| `delete` | Removes the record and, when live, stops and removes its module instance. It does not close the shared Phone Link session. |

A live command uses the Unix control socket. If the socket is absent, refused, or not a socket, the command falls back to offline registry file behavior. If the runtime is starting, it returns `runtime is starting; retry the feature command` instead of making an offline edit. See [configuration and state](configuration.md).

## Modular runtime: `run`

| Flag | Default | Effect |
| --- | --- | --- |
| `--state PATH` | `~/.config/phonelink-linux/state.json` | Authentication and enrollment state. |
| `--features-state PATH` | `~/.config/phonelink-linux/features.json` | Desired feature registry. |
| `--app-version VERSION` | `1.26072.116.0` | CrossDevice app metadata. |
| `--ring NAME` | `Public` | CrossDevice ring metadata. |
| `--os-version VERSION` | `10.0.26100` | Windows-compatible OS metadata. |
| `--target SELECTOR` | Empty | Linked peer selector; empty requires exactly one linked Android peer. |
| `--signalr-timeout DURATION` | `10s` | SignalR `OnConnected` wait. |
| `--wake-timeout DURATION` | `45s` | Peer presence wait after wake. |
| `--wake-ttl DURATION` | `60s` | Dispatcher wake time-to-live. |
| `--request-timeout DURATION` | `10s` | Time to wait for the host's `/SessionValidation` response. Clipboard requests use the module's `request_timeout_ms` configuration. |

The runtime rejects non-positive timeout and TTL values. It creates a control socket derived from the feature store path, starts the shared host, loads and validates desired features, and then reports readiness. Only one runtime should own a feature store at a time.

## Compatibility clipboard command

`clipboard-sync` accepts all host flags from `run` except `--features-state`, plus:

| Flag | Default | Effect |
| --- | --- | --- |
| `--poll-interval DURATION` | `500ms` | Fallback clipboard polling interval. It must be between `50ms` and `60s`. |
| `--publish-initial` | `false` | Publishes the current Linux clipboard once after startup. |

The host flags are `--state`, `--app-version`, `--ring`, `--os-version`, `--target`, `--signalr-timeout`, `--wake-timeout`, `--wake-ttl`, and `--request-timeout`, with the `run` defaults above.

Unlike `run`, `clipboard-sync` also copies `--request-timeout` into its in-memory clipboard configuration. That value must satisfy the module's 100 through 120000 millisecond range.

## Systemd user-service commands

```text
phonelink-linux service install [--enable=true] [--start=true]
phonelink-linux service uninstall [--keep-binary]
phonelink-linux service start|stop|restart
phonelink-linux service enable|disable|status
phonelink-linux service logs [--follow] [--lines N]
phonelink-linux service import-environment
```

| Command or option | Behavior |
| --- | --- |
| `install` | Defaults to `--enable=true` and `--start=true`. `--enable=false` skips enabling; it does not disable an already enabled unit. `--start=false` skips starting or restarting; it does not stop an existing process. |
| `uninstall` | Removes the unit and installed binary by default. `--keep-binary` keeps `~/.local/bin/phonelink-linux`. Authentication state and feature registry files are not removed. |
| `start`, `stop`, `restart`, `enable`, `disable`, `status` | Take no additional arguments. `start` and `restart` import the current graphical environment and clear failed/start-limit state before invoking systemd. |
| `logs` | `--follow` follows new user-journal entries. `--lines N` defaults to `100` and must be non-negative. |
| `import-environment` | Imports supported graphical and XDG variables into the systemd user manager without starting the unit. |

The [systemd user service guide](../operations/systemd.md) describes unit readiness, environment import, lifecycle, and recovery.

The command implementations are [`cmd/phonelink-linux/main.go`](../../cmd/phonelink-linux/main.go), [`cmd/phonelink-linux/feature.go`](../../cmd/phonelink-linux/feature.go), [`cmd/phonelink-linux/runtime_run.go`](../../cmd/phonelink-linux/runtime_run.go), and [`cmd/phonelink-linux/service.go`](../../cmd/phonelink-linux/service.go). CLI behavior is exercised by [`cmd/phonelink-linux/feature_test.go`](../../cmd/phonelink-linux/feature_test.go), [`cmd/phonelink-linux/runtime_control_test.go`](../../cmd/phonelink-linux/runtime_control_test.go), and [`cmd/phonelink-linux/service_test.go`](../../cmd/phonelink-linux/service_test.go).
