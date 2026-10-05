# First run and enrollment

[Documentation index](../README.md)

Fresh installs use one Phone Link (`phonelink` / PL) enrollment and one Microsoft device-code sign-in for clipboard and notifications. The phone must already be linked in Link to Windows under that account. Clipboard sync needs a graphical clipboard provider; notifications need Android notification access and a desktop notification service.

LinkMyPhone does not discover or pair a new phone. It enrolls a Linux DCG identity, asks Microsoft for devices already linked to the account, and selects one of those peers.

| Command | Where it stops |
| --- | --- |
| `bootstrap-probe` | Sign in, enroll, save keys, refresh and save trust, then await account relay `OnConnected`. |
| `run` | Resume the saved profile, select and wake a peer, validate its session, and start enabled features. |


## 1. Create the persistent identity

Run:

```bash
linkmyphone bootstrap-probe
```

On a new state path, the command prints a Microsoft device-code verification URL and a one-time code. Open the URL in a browser, sign in with the account that owns the existing Link to Windows relationship, and wait for the command to continue. The PL enrollment serves both clipboard and notifications. The command then:

1. enrolls the Linux DCG device;
2. saves the new identity and refresh credentials;
3. retrieves linked-device metadata and builds trust relationships;
4. obtains an assigned SignalR shard and waits for the account-level Hub Relay connection.

The command does not print secrets. It saves state before later cloud stages, so resume a failure after enrollment rather than deleting files or creating another identity.


The default state path is `~/.config/linkmyphone/state.json`. Use another path when you intentionally maintain a separate account or test state:

```bash
linkmyphone bootstrap-probe --state "$HOME/.config/linkmyphone-test/state.json"
```

The remaining [bootstrap flags](../reference/cli.md) change compatibility metadata sent to Microsoft's service. Keep the defaults unless your deployment requires different values.

### Existing CrossDevice and trial Phone Link enrollments

Old `state.json` files without `clientProfile` and explicitly enrolled `crossdevice` (WEA) states remain WEA. Resume never changes their identity or keys. They still support clipboard, but cannot receive full notification push. **Do not edit `clientProfile` or overwrite a WEA state with PL keys.**

If you already have a PL trial state, reuse its `--state` path for `bootstrap-probe` and `run`. Enable **both** feature records in the same feature store; no second sign-in or enrollment is needed. Keep the WEA state as a backup until clipboard and notifications work on PL for your phone. Stop the old clipboard service before starting the combined runtime to avoid competing clipboard updates.

If you have only a WEA enrollment, leave it intact and create a *new* PL enrollment at another `--state` path. This upgrade requires one new device-code sign-in; subsequent runs use that PL enrollment for both features. It does not migrate WEA trust or OAuth credentials. The feature store is separate from authentication: use `feature create`/`feature enable` against the store selected by `run --features-state`.

Grant Link to Windows notification access on the phone and run a desktop notification service on Linux. Notifications will not report Ready if either is missing or the phone connection fails. New phone items update the matching desktop notification; phone removals close it. Existing phone items stay quiet at startup unless you change `show_existing`.

Set `remote_actions=true` to allow desktop dismissal, app buttons and confirmed replies to affect the phone. With it off, notifications are receive-only. Timeout and programmatic closes do not dismiss phone notifications. Replies need Python GI/GTK4 and send text only after confirmation. A successful phone response means Android accepted the request, not that a message reached its recipient. See [notification settings](../reference/configuration.md#notification-configuration) and [privacy](../operations/privacy-and-state.md#notifications).

On S23/CachyOS/Wayland, one PL sign-in ran both modules together; clipboard transfers, notification delivery, Like, reply and dismissal worked. Permission revocation, forced network loss, long-running recovery and other phones remain untested. See [test results](../research/validation.md#notification-desktop-actions-on-unified-enrollment-2026-10-05).

## 2. Resume an existing enrollment

Run `bootstrap-probe` again, or start `run`, `peer-probe`, or `session-probe` with the same state path. The program loads the saved state, refreshes the Microsoft token, signs the existing DCG identity in, refreshes linked-device trust, and reconnects SignalR. Resume calls DCG `SignIn`; it does not create a second identity.

A missing state file is an enrollment problem, not a clipboard problem. Run `bootstrap-probe` first. A malformed or unsupported state file is refused so that the program does not silently create a different identity.

## 3. Select the linked phone

Without `--target`, startup selects the only linked Android device. It fails if none or more than one is returned.

Select a target by its DCG ID, exact name, exact model name, or an unambiguous case-insensitive partial match:

```bash
linkmyphone run --target "My phone"
```

The selector matches only devices returned by Microsoft's `DeviceInfoList`; it cannot pair a new device. Ambiguous matches are rejected. The host checks Hub Relay presence, sends a signed wake request if needed, and performs PLATFORM `/SessionValidation` before starting feature modules.

An explicit selector can match any returned linked peer, including a non-Android device. This does not guarantee compatibility. Recorded clipboard checks used an S23; other peer types are unvalidated.

## 4. Enable clipboard and notifications in one runtime

Create both built-in feature records in the **same** feature store, then run once with the **same** PL state:

```bash
linkmyphone feature create --enabled linkmyphone.clipboard
linkmyphone feature create --enabled --config '{"remote_actions":false}' linkmyphone.notifications
linkmyphone run
```

Put options before the feature ID: the CLI uses Go's standard flag parser. Without `--enabled`, `feature create` creates a disabled record. `run` starts both modules on one shared Phone Link host session.

If a record exists, inspect it with `feature get ID` and enable it with `feature enable ID`. For a PL state at another path, use `run --state PATH`. For a separate feature store, pass the same `--state FEATURE_PATH` to both feature commands and `--features-state FEATURE_PATH` to `run`.

Check desired and live state from another terminal while the runtime is ready:

```bash
linkmyphone feature list
linkmyphone feature get linkmyphone.clipboard
```

Startup waits for authentication, trust refresh, peer wake, SignalR, and SessionValidation. Feature commands return `runtime is starting; retry the feature command` until the runtime announces readiness. Retry then. See [clipboard behavior](../user-guide/clipboard.md) for backend selection and initial publication.

## 5. Check both clipboard directions

1. Keep `run` open until the host and clipboard module report readiness.
2. Copy fresh, non-sensitive text on Linux and paste it into a text field on the phone.
3. Copy different harmless text on the phone and paste it into a Linux text editor.
4. Check the runtime's direction and byte-count events. The logs intentionally omit the copied text.
5. Press Ctrl+C and confirm that the module stops cleanly.

Also copy a formatted HTML fragment and an image in each direction. Use an HTML-capable paste target for formatting and the same image file when comparing Linux with Windows. Check dimensions and successful paste; rich content support is documented in the [clipboard guide](../user-guide/clipboard.md).

The clipboard value present before startup is not sent by default. Test with a new copy after readiness, and avoid passwords or private text while synchronization is enabled. The [clipboard guide](../user-guide/clipboard.md) explains initial publication, clear events, conflicts, and reflected updates.

For diagnostic probes and their effects, see the [CLI reference](../reference/cli.md#authentication-and-probes).

## Connectivity and recovery

An internet connection is required for Microsoft sign-in, device trust, relay and wake. `run` reconnects after network loss or suspend and restarts enabled modules. See [session recovery](../operations/session-recovery.md) for what to expect during an interruption, and [test results](../research/validation.md) for the tested devices and remaining checks.
