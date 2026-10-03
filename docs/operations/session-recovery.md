# Session recovery

[Documentation index](../README.md) · [Runtime flags](../reference/cli.md#modular-runtime-run) · [Implementation](../architecture/session-resilience.md)

The long-running commands, `run` and `clipboard-sync`, now recover cloud sessions inside the same process. Recovery refreshes credentials and device trust, reconnects Hub Relay, wakes the original phone when needed, completes PLATFORM SessionValidation, and recreates enabled feature modules.

**Status remains Partial:** the owner reports successful recovery after Linux network loss, phone network loss and real suspend/resume on the S23/Wayland setup, including fresh bidirectional clipboard transfers afterwards. The full race suite also passed on the owner's machine. Scheduled renewal through actual token expiry and multi-hour reliability still need distinct evidence; see the dated [validation record](../research/validation.md#session-resilience-2026-10-03).

## What triggers recovery

| Trigger | Runtime behavior |
| --- | --- |
| Relay read/write failure or server close | Stop the old feature generation and reopen the session. |
| A connected socket stops receiving hub traffic | A 45-second server-silence deadline interrupts the read. Successful client writes alone do not establish a healthy connection. |
| Phone disconnects, including a quick disconnect/reconnect | The five-second host monitor detects presence loss or its retained disconnect counter and recreates the PLATFORM session and features. |
| Suspend or a large wall-clock gap | The monitor detects gaps over 20 seconds on its next tick and recreates the session. |
| Earliest Microsoft/DCG token approaches expiry | Schedule a new session before expiry. The default margin is two minutes, capped at 20% of the token lifetime. |
| Temporary startup failure | Retry with exponential backoff and jitter, capped at a one-minute base delay. |
| Throttling | Honor a valid Retry-After delay even above the cap; HTTP 429 without a usable header waits at least one minute. |

The relay sends MessagePack Hub Protocol keepalives every 15 seconds. A full connection/negotiate/protocol handshake has a 15-second default deadline, writes have a five-second deadline, and one complete session-opening attempt has a two-minute default deadline.

The runtime pins the first successfully opened phone by DCG ID. It does not silently pick another device if discovery changes. If the selected phone is no longer linked, target selection fails explicitly.

## During an interruption

The process logs `session recovering` with the retry delay and a redacted failure category. The feature control socket stays reserved; feature commands return `runtime is recovering; retry the feature command`. They do not switch to offline edits while recovery owns the feature store. Successful desired-state changes completed before the handoff are loaded into the replacement registry.

Old endpoints, request state and live module instances are retired before replacement modules start. Their registry epochs are local to that registry; a new registry can begin its epoch sequence again. Do not interpret an epoch as a globally increasing session ID.

Clipboard FEATURE_ON synchronization runs again when its module starts. Interrupted requests and clipboard snapshots are not replayed into the new session. With the default `publish_initial=false`, copy a new selection after readiness to send it. If you explicitly enable `publish_initial`, the current selection is published on each module start, including recovery.

## Errors that need intervention

Malformed/missing local enrollment, credential-persistence failure, mismatched identity and permanent OAuth/DCG authorization errors stop recovery. A relay HTTP 401 gets one new session-opening attempt with refreshed credentials; a repeated 401 stops. Revoked Microsoft refresh credentials (`invalid_grant`) need account reauthorization; repeatedly restarting the service cannot make them valid.

Automatic recovery preserves the enrolled identity and keys. It never invokes CreateIdentity or deletes the state file. Keep the existing state and inspect the first failing authentication stage. The current bootstrap command resumes an existing state; it does not offer an interactive reauthorization flag for a revoked refresh credential.

`bootstrap-probe`, `peer-probe` and `session-probe` remain finite diagnostics. They do not enter the long-running supervisor.

## Live checks

Use the existing enrollment and feature store, one runtime, and harmless clipboard selections. Record whether the same process returns to readiness and a newly copied selection works in both directions.

| Scenario | Check |
| --- | --- |
| Network loss | Disconnect Linux networking for more than 45 seconds, restore it, and verify fresh bidirectional clipboard transfers after recovery. |
| Phone offline | Disable the phone's network, then restore it. Verify wake/re-presence, SessionValidation and FEATURE_ON before fresh transfers. |
| Suspend/resume | Suspend Linux for at least a minute, resume, and verify a rebuilt session and fresh transfers. |
| Token renewal | Run past the actual token lifetime, observe the scheduled session renewal, and verify transfers afterwards. A larger refresh margin can move renewal earlier, but is capped at 20% of the lifetime. |
| Feature state | Disable a module before an interruption; confirm it stays disabled afterwards. After recovery, enable it and confirm live CRUD still works. |
| Stop while offline | Stop during an attempt or backoff; confirm the runtime and clipboard workers exit without waiting for networking. |
| systemd | Install the updated unit, repeat loss/recovery, and confirm the process recovers without consuming systemd's restart limit. |

Inspect logs with `linkmyphone service logs --follow` for a service or use foreground `linkmyphone run`. A blocking systemd start can wait while offline; `systemctl --user --no-block start linkmyphone.service` returns immediately. The updated unit removes the outer startup timeout while retaining per-attempt deadlines.

The 2026-10-03 owner report at `a1f53ff` covers Linux network loss, phone network loss and suspend/resume. The supplied foreground log shows three recovery cycles returning to PLATFORM and module readiness, unchanged displayed device IDs and control-socket path, and restoration of disabled feature state followed by a successful enable. The owner confirms the live checks and clipboard operation in both directions after recovery. These are owner-reported results; the agent did not repeat them against Microsoft services. The transcript does not label the order or timing of individual faults.

A `FEATURE_OFF synchronization failed` warning with `endpoint is closed` can occur while retiring a failed session: the host closes its transport before stopping old modules. The replacement module synchronizes FEATURE_ON again. Evaluate the subsequent readiness and fresh clipboard transfer when checking recovery.

The approximately 15-minute foreground transcript does not separately establish scheduled renewal through actual token expiry, a multi-hour soak, or recovery under the updated systemd unit. Record those outcomes in [validation](../research/validation.md#session-resilience-2026-10-03) before promoting the combined support label.
