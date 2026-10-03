# Session recovery

[Documentation index](../README.md) · [Runtime flags](../reference/cli.md#modular-runtime-run) · [Implementation](../architecture/session-resilience.md)

`run` and `clipboard-sync` reconnect automatically after network loss, a phone disconnection or suspend. They refresh authentication, reconnect the relay, wake the same phone when needed, and restart enabled modules once PLATFORM SessionValidation succeeds. The process keeps its enrollment and device keys.

## What triggers recovery

| Trigger | What happens |
| --- | --- |
| Relay read/write failure or server close | Close the failed session and reconnect. |
| No incoming Hub traffic for 45 seconds | The read times out and starts recovery. |
| Phone disconnects | The five-second monitor starts recovery. A quick disconnect/reconnect is also detected. |
| Suspend or a wall-clock gap over 20 seconds | Reopen the session on the next monitor tick. |
| Microsoft or DCG token nears expiry | Reopen before the earlier expiry. The default margin is two minutes, capped at 20% of the token lifetime. |
| Temporary startup failure | Retry with backoff, starting at one second and growing to a one-minute base delay. Each wait is between half and all of that delay. |
| HTTP 429 | Wait for Retry-After, even if it exceeds one minute. Without a valid header, wait at least one minute. |

Hub pings go out every 15 seconds. The handshake has a 15-second timeout, writes have a five-second timeout, and one complete session-opening attempt can take up to two minutes. The [runtime flags](../reference/cli.md#modular-runtime-run) let you adjust startup and recovery settings.

After the first successful connection, the runtime keeps the selected phone's DCG ID. Removing that phone from the account causes target selection to fail.

## During an interruption

The log prints `session recovering` with a reason and retry delay. Wait for `PLATFORM session ready` and `[OK] Modular runtime is running` before testing another transfer.

`run` keeps the control socket open throughout recovery. Feature commands return:

```text
runtime is recovering; retry the feature command
```

Retry the command after readiness. Changes saved before the interruption, including disabled modules and their configuration, are loaded into the new session.

The clipboard module sends FEATURE_ON again when it starts. With the default `publish_initial=false`, **copy a new selection after readiness** to send it. Setting `publish_initial=true` sends the current selection on every module start, including after recovery. Interrupted requests and old snapshots are discarded.

You may see `FEATURE_OFF synchronization failed` with `endpoint is closed` during teardown. The failed session has already closed its transport, so the old module cannot send FEATURE_OFF. Check that the replacement sends FEATURE_ON and reaches readiness.

## Errors that need intervention

Recovery stops for invalid or missing enrollment, failed state-file writes, an identity mismatch, or permanent authentication errors. Check the first failing stage and keep the existing state file while investigating.

`invalid_grant` means the Microsoft refresh credential needs reauthorization. The current `bootstrap-probe` resumes existing state and has no interactive reauthorization option. Repeated service restarts cannot restore a revoked credential.

A relay HTTP 401 gets one retry with refreshed credentials. A second 401 stops the runtime.

`bootstrap-probe`, `peer-probe` and `session-probe` exit after their diagnostic stages. Automatic recovery runs in `run` and `clipboard-sync`.

## Live checks

Use one runtime with the existing enrollment. After each interruption, wait for readiness, copy a new selection and paste in both directions.

| Test | Steps |
| --- | --- |
| Linux network loss | Disconnect for at least 90 seconds, then reconnect. |
| Phone network loss | Disable the phone's networking, then restore it. |
| Suspend/resume | Suspend Linux for at least a minute, then resume. |
| Feature state | Disable clipboard before an interruption. Check that it stays disabled after recovery, then enable it and test another transfer. |
| Token renewal | Leave the runtime running past the token lifetime. Check that it renews the session and transfers still work. The refresh margin is capped at 20%, even if you set a larger value. |
| Stop while offline | Stop during an attempt or backoff. Check that the process and clipboard workers exit. |
| systemd | Install the updated unit and repeat the interruption tests. The main process should recover without a systemd restart. |

Network loss on both devices, suspend/resume and feature-state recovery passed on the S23/Wayland setup at `a1f53ff`. Testing across token expiry, a multi-hour run and recovery under the updated systemd unit is still open. See [test results](../research/validation.md#session-resilience-2026-10-03).

View service logs with `linkmyphone service logs --follow`, or run `linkmyphone run` in a terminal. During offline startup, `systemctl start` waits for readiness. Use `systemctl --user --no-block start linkmyphone.service` to return immediately while it connects.
