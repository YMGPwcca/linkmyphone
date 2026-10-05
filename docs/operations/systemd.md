# systemd user service

[Documentation index](../README.md)

The optional systemd integration runs `linkmyphone run` as a per-user service tied to the graphical login. It is not a machine-wide daemon.

## Install and start

Build the executable, then install the service using that binary:

```bash
go build -o linkmyphone ./cmd/linkmyphone
./linkmyphone service install
```

The installer:

- copies the current executable atomically to `~/.local/bin/linkmyphone`;
- writes the unit to `$XDG_CONFIG_HOME/systemd/user/linkmyphone.service`, or `~/.config/systemd/user/linkmyphone.service`;
- imports the supported graphical-session environment;
- reloads the systemd user manager;
- when starting, clears previous failed/start-limit state;
- enables the unit and restarts it onto the newly copied executable by default.

Use flags before any positional arguments:

```bash
./linkmyphone service install --start=false
./linkmyphone service install --enable=false
```

`--start=false` installs and reloads without starting. `--enable=false` skips automatic graphical-session startup. These flags do not disable an already enabled unit or stop a running process.

The installed unit executes `%h/.local/bin/linkmyphone run`, so authentication and feature paths come from the service user's environment and defaults.

Stop an interactive `linkmyphone run` before starting the service. Both use the same per-feature-store control socket; only one runtime can own it.

## Manage the service

```bash
~/.local/bin/linkmyphone service status
~/.local/bin/linkmyphone service start
~/.local/bin/linkmyphone service restart
~/.local/bin/linkmyphone service stop
~/.local/bin/linkmyphone service enable
~/.local/bin/linkmyphone service disable
```

`start` and `restart` import the current graphical-session environment, clear failed/start-limit state, and invoke `systemctl --user`.

To refresh environment values after a desktop session change:

```bash
~/.local/bin/linkmyphone service import-environment
~/.local/bin/linkmyphone service restart
```

The imported variables are `WAYLAND_DISPLAY`, `DISPLAY`, `XAUTHORITY`, `XDG_SESSION_TYPE`, `XDG_CURRENT_DESKTOP`, `XDG_SESSION_DESKTOP`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and `XDG_DATA_HOME`. `XDG_RUNTIME_DIR` is not imported; it belongs to the systemd user manager and login session.

The unit is `Type=notify`. Readiness waits for the control socket, Phone Link host, desired feature loading, and live feature controller. `systemctl start` waits through authentication, trust refresh, peer presence, SessionValidation, and module startup. Feature commands return `runtime is starting; retry the feature command` while the unit is activating.

| Transition | Behavior |
| --- | --- |
| Start | Stay `activating` until the runtime sends readiness. |
| Transient cloud/peer failure | Recover inside the same process with host backoff; the socket remains reserved. |
| Terminal process failure | systemd retries after 5 seconds, subject to the five-starts-per-60-seconds limit. |
| Restart | Stop modules and watchers, then activate again. |
| Explicit stop | Stop modules and watchers without an automatic restart. |


The packaged unit has these lifecycle settings:

```ini
Type=notify
Restart=on-failure
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=5
TimeoutStartSec=infinity
TimeoutStopSec=20s
KillMode=mixed
UMask=0077
```

The unit is enabled under and joined to `graphical-session.target`. The main process receives the normal stop signal and shuts down modules and the clipboard watcher. If shutdown exceeds the timeout, systemd can terminate remaining cgroup processes.

## Use a custom profile or phone target

Service helpers do not remember foreground `run` options. The packaged `ExecStart` uses defaults. To use another account state, feature store, or phone selector, configure a systemd drop-in rather than editing the installed unit.

First prepare the enrollment and enabled feature record at the chosen paths, following [first-run setup](../getting-started/first-run.md) and [configuration](../reference/configuration.md). Then install without starting or enabling the default command:

```bash
./linkmyphone service install --enable=false --start=false
systemctl --user edit linkmyphone.service
```

Example drop-in for a test profile and a specific linked phone:

```ini
[Service]
ExecStart=
ExecStart=%h/.local/bin/linkmyphone run --state %h/.config/linkmyphone-test/state.json --features-state %h/.config/linkmyphone-test/features.json --target "My phone"
```

`%h` is systemd's home-directory specifier. The unit does not invoke a shell. Use paths that contain the intended enrollment and feature records; changing `ExecStart` does not create them.

Stop any foreground runtime using that store, then reload and start:

```bash
systemctl --user daemon-reload
~/.local/bin/linkmyphone service enable
~/.local/bin/linkmyphone service restart
```

Reinstallation replaces the packaged unit and executable but does not remove your drop-ins. Inspect the effective command with `systemctl --user cat linkmyphone.service` when diagnosing a path or target mismatch.

## Logs and start-limit recovery

Service output goes to the user journal:

```bash
~/.local/bin/linkmyphone service logs
~/.local/bin/linkmyphone service logs --follow
~/.local/bin/linkmyphone service logs --lines 250
journalctl --user -u linkmyphone.service --no-pager
```

Desktop notification dismissal produces journal entries for each step:

```text
Dismissed notification #41 on desktop. Asking the phone to remove it.
Phone accepted dismissal of notification #41.
Removed notification #41 after a phone update. 16 left.
```

Notification numbers are temporary and change after a service restart. Button presses and replies have their own entries. Logs never include notification text, keys or reply text. "Phone accepted" means Android accepted the request; check the receiving app to confirm reply delivery.

`service install`, `service start`, and `service restart` call `systemctl --user reset-failed` before launching. If you use `systemctl` directly after repeated failures, clear the rate limit first:

```bash
systemctl --user reset-failed linkmyphone.service
~/.local/bin/linkmyphone service import-environment
~/.local/bin/linkmyphone service start
```

Common startup failures include a missing enrollment file, no linked target, an unavailable clipboard provider, missing graphical-session variables, or invalid feature configuration. Read the first failure stage in the journal before changing state. See [troubleshooting](troubleshooting.md).

## Uninstall and persistence

```bash
~/.local/bin/linkmyphone service uninstall
```

Uninstall stops and disables the unit, removes the user unit and installed binary, reloads the user manager, and clears failed state. To remove the unit while keeping the installed executable:

```bash
~/.local/bin/linkmyphone service uninstall --keep-binary
```

Uninstall does not remove `state.json`, `features.json`, or other authentication and desired-state files. Remove those separately only when you intentionally want to discard the local identity and feature configuration. See [privacy and state](privacy-and-state.md).

Custom systemd drop-ins are also retained. Review any `linkmyphone.service.d/` directory under your user unit configuration before reinstalling; remove only overrides you intentionally want to discard.

## Validation and recovery limits

Installation, readiness, restart, logout/login, clipboard watchers and live feature control have been tested on CachyOS/Wayland. The commands and results are in [validation](../research/validation.md).

Temporary cloud or phone failures recover in the same process. systemd restarts the process after a terminal failure. `TimeoutStartSec=infinity` lets offline startup keep retrying; each session attempt still has a two-minute default timeout. Use `systemctl --user --no-block start linkmyphone.service` to start without waiting for readiness. See [session recovery](session-recovery.md) for interruption tests.

See the [source map](../developer/source-map.md) for implementation ownership.

## Updating an older phonelink-linux installation

Older installations may use `phonelink-linux` and `phonelink-linux.service`. `linkmyphone service stop` targets only `linkmyphone.service`. Stop the old unit before running the new foreground binary:

```bash
systemctl --user stop phonelink-linux.service
./linkmyphone clipboard-sync --state "$HOME/.config/phonelink-linux/state.json"
```

This reuses the enrollment and starts the current clipboard module without editing the feature registry. For a permanent update, stop the foreground process and configure the new unit's state path before installing/starting it. Keep only one clipboard runtime active; see the custom-profile instructions above.
