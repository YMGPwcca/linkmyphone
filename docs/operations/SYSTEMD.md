# systemd user service

Phone Link Linux can install itself as a per-user systemd service. The service runs the same modular `run` command used interactively, so the feature control socket and live CRUD behavior are unchanged.

## Install

From a checked-out tree:

```bash
go run ./cmd/phonelink-linux service install
```

The installer:

- copies the currently-running executable atomically to `~/.local/bin/phonelink-linux`;
- writes `phonelink-linux.service` to `$XDG_CONFIG_HOME/systemd/user/` (or `~/.config/systemd/user/`);
- imports the current graphical-session environment that matters to Wayland/X11 and XDG paths;
- reloads the systemd user manager;
- clears any previous failed/start-limit state before startup;
- enables the unit and restarts it onto the freshly installed binary by default.

Use `--start=false` or `--enable=false` when packaging without immediately starting/enabling it.

Stop any manually-running `phonelink-linux run` process before starting the service. Both modes intentionally own the same per-feature-store control socket, so a second runtime should fail rather than create split-brain state.

## Lifecycle

```bash
~/.local/bin/phonelink-linux service status
~/.local/bin/phonelink-linux service restart
~/.local/bin/phonelink-linux service stop
~/.local/bin/phonelink-linux service start
~/.local/bin/phonelink-linux service disable
~/.local/bin/phonelink-linux service enable
```

`start` and `restart` import the current graphical-session environment before asking systemd to launch the service. If the desktop session changes its Wayland/X11 environment after login, refresh it explicitly:

```bash
~/.local/bin/phonelink-linux service import-environment
~/.local/bin/phonelink-linux service restart
```

The imported set is intentionally narrow: `WAYLAND_DISPLAY`, `DISPLAY`, `XAUTHORITY`, session desktop/type fields, and persistent XDG config/state/data roots. `XDG_RUNTIME_DIR` is not imported because it belongs to the systemd user manager/login session.

`service install`, `service start`, and `service restart` clear systemd's failed/start-limit state before launching. Re-running `service install` also uses `restart` rather than `start`, so an already-running service switches to the newly copied executable instead of continuing with the old process.

An empty Wayland clipboard is a valid initial state. `wl-paste` reports it as exit status 1 with `Nothing is copied`; the native text backend normalizes that specific condition to an empty string so the clipboard module can still reach Ready and wait for future watcher events.

## Logs

The unit writes stdout/stderr to the user journal:

```bash
~/.local/bin/phonelink-linux service logs
~/.local/bin/phonelink-linux service logs --follow
~/.local/bin/phonelink-linux service logs --lines 250
```

Equivalent native command:

```bash
journalctl --user -u phonelink-linux.service -f
```

## Restart and shutdown policy

The packaged unit uses `Type=notify`. `systemctl start` does not report success until the runtime has bound its control socket, opened the Phone Link host, loaded desired feature state, and switched the control handler to the live controller. This keeps service startup from reopening the CLI's offline-fallback race.

The packaged unit also uses:

- `Restart=on-failure`;
- `RestartSec=5s`;
- at most 5 starts per 60 seconds before systemd rate-limits a persistent failure;
- `TimeoutStartSec=90s`;
- `TimeoutStopSec=20s`;
- `KillMode=mixed`.

`KillMode=mixed` is deliberate. On a normal stop, systemd sends the initial termination signal only to the main Phone Link Linux process. The runtime then performs its normal module shutdown and cancels the Wayland watcher itself. If shutdown exceeds the timeout, systemd can still kill remaining processes in the service cgroup.

The unit is enabled under `graphical-session.target` and is also `PartOf` that target, so it follows the graphical login session instead of behaving like a machine-wide daemon.

## Live feature control

The daemon still exposes the same Unix control plane. While the service is running:

```bash
~/.local/bin/phonelink-linux feature list
~/.local/bin/phonelink-linux feature disable phonelink.clipboard
~/.local/bin/phonelink-linux feature enable phonelink.clipboard
```

These commands reconcile the running service immediately rather than editing desired state offline.

## Live validation status

The install/start path has been validated on CachyOS with a Wayland session:

- `service install` successfully installed, enabled, and started the user unit;
- `Type=notify` reported `Phone Link Linux runtime ready` only after the phone host, clipboard module, live capabilities, and control socket were ready;
- `feature list` observed the running daemon through the live control plane;
- `wl-paste --watch` ran as a child inside the service cgroup;
- startup succeeded when the Wayland clipboard initially had no selection;
- the related targeted tests, race tests, full test suite, and full race suite passed.

Restart and explicit stop/start have also been live-validated:

- `service restart` waited through the readiness handshake and returned with the replacement runtime already `active (running)`;
- the replacement runtime recreated `wl-paste --watch`, Ready capabilities, and the live control socket;
- `service stop` shut the module down cleanly with exit status 0 and no watcher fallback;
- after stop, no Phone Link Linux `phonelink-linux run` or clipboard watcher process remained; unrelated desktop clipboard-manager watchers were unaffected;
- `service start` then recreated the runtime and returned only after `Phone Link Linux runtime ready`.

Graphical-session logout/login has also been live-validated:

- logout stopped the service with status 0 and the clipboard module emitted `module stopped`;
- no Phone Link Linux runtime or watcher remained after session shutdown;
- login started the enabled service automatically;
- the new session's `XDG_RUNTIME_DIR` and `WAYLAND_DISPLAY` were available to the user manager;
- the service progressed through `activating` to `Phone Link Linux runtime ready`, recreated `wl-paste --watch`, and restored the live control socket;
- a feature CLI call issued during `activating` returned the explicit startup retry response rather than silently falling back to offline state, and returned live state once readiness completed.

The systemd user-service lifecycle matrix is therefore live-validated end to end: install, empty-clipboard startup, restart, explicit stop/start, graphical logout, and graphical login recovery.

## Uninstall

```bash
~/.local/bin/phonelink-linux service uninstall
```

This stops/disables the unit, removes the user unit and installed binary, reloads the user manager, and clears failed state. To keep the installed executable:

```bash
~/.local/bin/phonelink-linux service uninstall --keep-binary
```
