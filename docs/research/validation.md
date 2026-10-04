# Validation and safe probes

[Documentation index](../README.md)

This page records manual device tests and automated checks. Older results come from the [baseline README at `e51728b`](https://github.com/YMGPwcca/phonelink-linux/blob/e51728b12a148061c8076c67200ee4a8f366e423/README.md); the dated sections below add the later clipboard and recovery runs. Command examples use the current LinkMyPhone names.

## Current results

| Area | Result |
| --- | --- |
| Sign-in, enrollment and device trust | Working with the linked S23. |
| Relay, wake and SessionValidation | Passed on S23/Wayland. |
| Text, HTML and image clipboard | Paste tested in both directions. |
| Network loss and suspend | Recovery passed without restarting the process. |
| Feature CRUD and systemd service | Lifecycle checks passed on CachyOS/Wayland. Recovery under the updated service unit still needs testing. |
| Token renewal | Scheduling and persistence tests pass; a run across actual token expiry is still open. |

Authenticated X11 runs, other phones and other desktop setups still need testing. The [research method](method.md) describes the capture procedure.

## Unified PL enrollment (2026-10-05)

PR [#9](https://github.com/YMGPwcca/linkmyphone/pull/9) merged notification synchronization into `main` before this change. A fresh `bootstrap-probe` now chooses PL for its single device-code sign-in; a saved WEA enrollment remains WEA and is never relabeled. The previously enrolled PL profile was reused on the CachyOS desktop without a new sign-in.

With the existing PL state, `session-probe --context-probe` reached S23 clipboard STATUS and CONTENT with matching correlation IDs. The probe answered FEATURE_ON and declined CONTENT, **sending no clipboard bytes**. In a separate combined runtime, both `linkmyphone.clipboard` and `linkmyphone.notifications` reached Ready in one host generation. Clipboard FEATURE_ON returned status 1; notifications APP connect reported `ecr=true` and reconciled six items with remote actions disabled. `feature get` reported both records Ready, epoch 1, and exposed clipboard text/HTML/image and notification receive capabilities.

Linux `go test -race ./...`, `go vet ./...` and CLI build passed on the isolated build tree; focused bootstrap/CLI/state tests passed after the final test cleanup. Tests assert the fresh enrollment's `PL` metadata/app ID, persisted profile and refusal to reclassify an existing WEA path. No fresh Microsoft device-code login was performed for this change; the real run reused the existing PL identity.

Bidirectional plain-text delivery also passed while **both modules were Ready on the PL state** and the old WEA service was stopped. A temporary Android foreground fixture set a harmless test string; the PL runtime reported `phone -> Linux applied clipboard (bytes=28)` and the Linux selection's SHA-256 matched the fixture string. A second harmless Linux selection produced `Linux -> phone published clipboard (bytes=33)`; the foreground Android fixture compared the received clipboard to the expected value and logged `PC_TO_PHONE_MATCH` without printing it. This validates both directions on this S23 with its existing Windows pairing; HTML/image and other phones under PL remain untested. The earlier source concern remains a compatibility risk in other connection-sharing configurations, not a failure observed here.

The old WEA service was restored to Active/Running. The desktop's original PNG selection was restored byte-for-byte (matching SHA-256) before removing the private backup. The Android fixture was uninstalled and its test clipboard was cleared; **its first backup read ran before the fixture gained foreground focus and reported an empty clip, so it cannot prove the phone's pre-test clipboard was empty**. Any prior phone clipboard value may need to be recopied. No phone network, account, notification permission or enrollment was changed.

## APP route decoding after fresh login (2026-10-05)

The user's first fresh PL login and one-host runtime reached notification APP ready/reconcile and clipboard transfers. After a ninth notification item, the runtime printed one `malformed APP envelope`. That output proves an APP payload failed the old universal PBValueSet decoder; **no raw envelope or route was captured**, so its exact cause cannot be assigned.

APP is multiplexed by `_route`. The supplied Windows `DeviceProxyMessageReceiver` handles `/DeviceProxyClient/TransportMiddleware` with a JSON body, whereas notification `/legacy/phonecontent` uses PBValueSet. The old notification receive loop decoded PBValueSet before inspecting `_route`, misreporting an unrelated valid JSON APP frame as malformed. An offline test reproduced that false warning before the change and passed after route-first decoding. The notification client now validates framing, ignores routes and response IDs it does not own, then decodes typed values only for its own routes. Malformed frames or typed bodies on a claimed route still report a non-content `stage` and decoder `reason`. This prevents the established false-positive shape; the user's exact live frame has **not** been re-captured or asserted fixed.

## Notification validation (2026-10-04)

Implementation on `feature/notification-sync`; source corpus: Phone Link 1.26072.257.0 and active Link to Windows 1.26082.130.0. No Microsoft source, application assemblies or captured personal notification payloads are tracked in the repository.

| Check | Observed result |
| --- | --- |
| Independent PBValueSet interoperability | Original Windows protocol assembly decoded synthetic Go Double, Int32/Int64 arrays, empty string-array elements and nested Bool; Go decoded its serialized response. |
| APP/state regressions | Typed correlated responses, malformed aligned arrays, duplicate pushes, silent existing state, key/post-time pairing, session reset, failed-render retry, stale actions/replies and non-user close reasons covered. |
| Real native desktop | A synthetic local peer drove real Quickshell D-Bus Notify/replacement/removal on CachyOS/Wayland; replacement retained its native ID, phone removal did not echo a mutation and teardown completed. No Microsoft/phone network was involved. |
| Actual GTK reply UI | Own native window inspected with cropped screenshots and AT-SPI. Confirmed `Xin chào 👋\\nSecond line` through its editor and Reply button; the synthetic peer received exact text and Android action index 4. |
| Linux verification | Full tests, full race suite, vet and CLI build passed on the isolated staging checkout. Working clipboard state/service were not replaced. |
| Native Stop ordering | Constructor cancellation initially closed D-Bus before local teardown. Reproduced on the real desktop; explicit backend lifetime now allows local notification close after feature cancellation, before backend Close. |
| Genuine PL enrollment | Separate PL device-code sign-in, identity enrollment, trust and Hub Relay succeeded. Existing WEA clipboard state/service remained separate. |
| S23 live receive/reconcile | APP connect succeeded with `ecr=true`; one real notification was synchronized. Module Ready exposed only `notifications.receive`, with `remote_actions=false`. |
| Android parser compatibility | Initial connect failed in Java lite `readInt64List` on an explicitly encoded zero-length packed array. Reproduced offline using the active Android schema and Java lite stream parser. Omitting empty repeated fields while preserving variant types fixed the offline parser and live S23 connect. |
| Live Android fixture mutations | Subsequently authorized and exercised on S23: explicit dismiss, explicit-key clear, launch, non-reply action and Unicode/multiline inline reply. A temporary fixture recorded actual Android receiver/activity effects; no personal notification was targeted. |
| Android action contract | Live fixture revealed that actions are serialized as `notificationActions`, not `actions`. Corrected the decoder and independent JSON fixture; action/reply then executed on the phone. |
| Live update/remove/restart | Fixture update retained the Android key with latest text; phone-originated removal disappeared after reconcile. Restarted the PL client after creating/updating notifications offline; both arrived as Existing with current text. |
| Ongoing notification | Android/LTW did not sync the fixture's ongoing notification. Local ongoing-dismiss protection is covered by automated tests, not claimed as a live mutation pass. |
| Expanded lifecycle regressions | User-dismiss reason 2, invalid explicit clear, original action indices, whitespace-preserving replies, concurrent prompt/action, request cancellation, permission result 7, queued stale mutations, retry after failed batch and teardown passed under Linux race detection. |
| Decoder fuzz | Bounded APP decoder fuzz ran for 15 seconds: 4,396,193 executions, no crash. Malformed nested values, variant type mismatches, array/recursion bounds and typed empty defaults have regressions. |
| D-Bus lifecycle/security | Actual isolated session bus with an independent notification service: literal markup escaping, authentic action/close events, spoofed-sender rejection, owner loss/rebind and local teardown after context cancellation passed. The user's Quickshell process was not restarted. |
| GTK cancellation | Actual native Cancel button exposed name/role through AT-SPI and returned no submitted text. Context cancellation terminated a second real GTK dialog without submitting. |
| WhatsApp self-chat | User selected Message yourself and confirmed sending did not generate a phone notification. Notification-key inspection found no WhatsApp notification; real-app inline reply could not be exercised. No other recipient was substituted. |
| Messenger real recipient | User authorized a conversation between their two Facebook accounts. A real Messenger notification was expanded in Quickshell using a virtual mouse; its horizontal action row was dragged to reveal Reply. The native reply editor received the approved Unicode test text and the actual confirmation button was clicked. The user confirmed the second account received the exact message. |
| Keyboard shortcuts | A temporary user-owned virtual input daemon delivered real keyboard events. Escape/Ctrl+Enter initially failed while the text editor held focus; moving the window key controller to capture phase fixed both. Actual GTK dialogs then canceled on Escape and submitted exact numeric test text on Ctrl+Enter. Initial alphabetic test text was transformed by the desktop Vietnamese IME, so the shortcut smoke used IME-neutral digits without changing user input settings. |
| Preserved device settings | Only the authorized fixture app/notification state was changed through ADB. Link to Windows notification-listener permission, phone network, account, Knox and bootloader were not modified. Permission loss remains an automated-test result. |
| Cleanup | Temporary Android fixture was uninstalled after clearing only its own notifications. Test runtimes exited cleanly; diagnostic scripts/APK/key material were removed. Existing clipboard service remained active with its original identity digest. |

Automation limits: the initial virtual-input prerequisite was resolved by starting a temporary daemon on a private user socket. Messenger reply mouse interaction and GTK Escape/Ctrl+Enter are now verified. Pointer-driven Quickshell dismissal remains distinct from the verified reply flow; global clear was not invoked because it would affect personal notifications. Notification status remains **Experimental**: live permission revocation, forced network loss, long-running recovery and broader device coverage remain unverified. Phone Wi-Fi was never disabled; ADB itself uses that connection.


## Clipboard validation (2026-10-03)

Setup: CachyOS/Wayland, `wl-clipboard`, Samsung Galaxy S23 with Link to Windows, and Windows Phone Link for the image comparison. HTML offers used GTK4/Python GI. The new executable reused `~/.config/phonelink-linux/state.json` through `--state`.

| Check | Result |
| --- | --- |
| Startup and content negotiation | PLATFORM ready, FEATURE_ON synchronized, module `0.2.0` ready with text/HTML/image capabilities. |
| Received HTML and text | Runtime applied `text/html` and `text/plain;charset=utf-8`; HTML paste worked. |
| Image transfer | Paste worked in both directions. Phone receive was checked after JPEG/BMP normalization was added at `2670f26`. |
| Earlier resize bug | One phone image pasted at 1523×2706 on Windows but 471×836 on Linux. |
| Receive after `7f999f3` | A new copy from the phone pasted at **1572×2096 on both PCs**. |
| Same-source outbound comparison | Linux → phone: **583×1036**, displayed as **1.05 MB**. Windows → phone: **310×551**, displayed as **330 KB**. |
| Automated checks | [Run 37098891014](https://github.com/YMGPwcca/linkmyphone/actions/runs/37098891014) at `7f999f3` passed vet, build, the full race suite and Xvfb text/HTML/image MIME transfers. |

Rich text & HTML clipboard and Image clipboard are **Supported** within the [documented formats and limits](../user-guide/clipboard.md). Received images keep their dimensions. The **1048576-byte (1 MiB)** PNG limit applies to outgoing images; displayed file sizes are rounded. Linux and Windows use different resizing and size-selection rules.

CI has run on main pushes only since `98a9e5c`. The clipboard CI run above predates that change.

## Session resilience (2026-10-03)

Tested at `a1f53ff` on a Samsung Galaxy S23 and CachyOS/Wayland. The maintainer ran the full race suite and the live checks below. Clipboard used `wl-clipboard` with polling; the foreground session lasted 14m40s.

| Check | Result |
| --- | --- |
| `go test -race ./...` | Passed across all packages, including Unix sockets and systemd notifications. The terminal prompt showed Go 1.27.1-X:nodwarf5. |
| CLI build | Passed. |
| Linux network loss and recovery | Passed. |
| Phone network loss and recovery | Passed. |
| Linux suspend/resume | Passed. |
| Clipboard after recovery | Paste worked in both directions. |
| Feature state | Clipboard stayed disabled across recovery, then could be enabled again. |
| Device identity and control socket | Displayed device IDs and socket path stayed the same. |

The log shows three recoveries reaching PLATFORM readiness and module startup within one `run` invocation. The test scenarios and successful paste results were confirmed after the run. [Session recovery](../operations/session-recovery.md) explains the teardown warning about FEATURE_OFF on a closed endpoint.

**Still to test:** scheduled renewal across token expiry, a run lasting several hours, and interruption tests under the updated systemd unit. Session recovery & refresh remains **Partial** until the token-expiry and extended-run checks are complete.

### Local fault tests

With Go 1.24.4, `go vet ./...`, the CLI build and the race tests passed for backoff, cancellation, transport timeouts, renewal scheduling, credential persistence and controller handoff. Test files are listed in the [implementation guide](../architecture/session-resilience.md#tests).

The development workspace blocks Unix sockets with `socket: operation not permitted`. Eleven tests failed on both the unchanged baseline and this branch; the rest of the suite passed with those tests excluded. The full suite subsequently passed on the maintainer's Linux machine, as recorded above.

<details>
<summary>Tests blocked by the workspace</summary>

```text
TestFeatureCommandCRUDForClipboard
TestFeatureCommandCanDisableAndDeleteStaleRecord
TestFeatureCommandDoesNotFallbackOfflineWhenRuntimeRejectsMutation
TestServerRoundTripAndSocketPermissions
TestCallUnavailable
TestListenReplacesStaleNonSocketPath
TestSwitchHandlerTransitionsWithoutReplacingSocket
TestServerCloseStopsServeWithoutContextCancellation
TestHandlerPanicReturnsFailureAndServerSurvives
TestSocketPathRemainsShortForDeepFeatureStore
TestReadySendsSystemdDatagram
```

No test or CI configuration was changed to skip them. CI runs on main pushes.

</details>

## Safety rules for every live probe

- Run only with an account and devices you control.
- Use a state directory intended for the experiment. Never publish the state file.
- Do not print JWTs, certificates, private keys, access tokens, refresh tokens, device codes, wake request data, enrollment JSON, raw platform payloads, phone payloads, or clipboard text.
- Use harmless unique test text. The explicit context probe is limited to 4096 bytes and reports byte counts only.
- Do not use sensitive text in command-line arguments; shell history can retain it.
- Do not run a second foreground runtime beside the user service against the same feature store.
- A successful HTTP wake, WebSocket write, Hub Completion, or DCG ACK proves only its own stage. A manual paste establishes the visible clipboard result.

## Historical stage results

### 1. Microsoft device-code login

**Probe**

```bash
go run ./cmd/linkmyphone bootstrap-probe
```

**Historical result**

The migrated CrossDevice scope and public client reached the Microsoft device-code endpoint and login succeeded. The probe acquired an MSA access token.

**Expected and failure signals**

A safe run shows a device-code message and continuation into DCG identity bootstrap. `authorization_pending` and `slow_down` are expected polling responses handled in [`auth/msa/devicecode.go`](../../auth/msa/devicecode.go#L103-L172). Expired codes, invalid client/scope, and HTTP failures prevent completion.

**Safety**

This is a live Microsoft authentication flow. Run only with an account and state directory intended for the experiment. Never run it with shared credentials, and never copy a token or verification code into logs or issue reports.

### 2. DCG identity creation and enrollment

**Probe**

Continue `bootstrap-probe` after device-code login.

**Historical result**

A DCG identity was created, a separate trust certificate was enrolled through `EnrollDevice`, and state was written before later cloud stages. The baseline reports device-enrollment success.

**Expected signals**

The run progresses past identity and enrollment, followed by a state file at the configured path. Current source requires an `accountCert` response before enrollment succeeds. The state directory and file use restrictive permissions.

**Failure signals and evidence**

Auth service HTTP errors, missing nonce/token/device ID, certificate mismatch, invalid enrollment metadata, missing account certificate, or missing DCG token stop this stage. Route and header boundaries are tested in [`auth/dcgauth/client_test.go`](../../auth/dcgauth/client_test.go#L13-L132) and [`auth/dcgauth/enroll_test.go`](../../auth/dcgauth/enroll_test.go#L12-L96).

Do not print JWTs, certificates, private keys, access tokens, refresh tokens, or full enrollment JSON. The tracked probe was designed to print safe metadata only.

### 3. Trust discovery and account relay

**Probe**

Continue `bootstrap-probe` after enrollment, or use saved state only in a controlled environment.

**Historical result**

`GetDeviceInfoList` returned linked Windows and Android peers, trust synchronization succeeded, assigned SignalR shard data was returned, and account-level `relayhub/` reached `OnConnected`.

**Expected signals**

Peer IDs are redacted or shortened, the selected region matches `OnConnected.RegionName`, and the relay reports the initial partner set. The implementation selects linked devices other than self and prefers peer `PKI` certificates over `SelfSigned`.

**Failure signals**

No assigned shard, no usable region, negotiate failure, binary WebSocket rejection, `OnConnected` timeout, or resolved-region mismatch. Shard and region handling are in [`bootstrap/cloud.go`](../../bootstrap/cloud.go#L73-L223).

The baseline reports success. It does not establish reconnect or token-refresh resilience.

### 4. Resume path

**Probe**

Run `bootstrap-probe` again with the same state path after a successful first run.

**Historical result and expected signals**

The intended restart path refreshes the MSA token and signs the existing DCG identity in. It does not call `CreateIdentity` again. The same local DCG identity is reused and a new service token is obtained; a refresh token may rotate. The source-level fake-service contract is tested in [`bootstrap/resume_test.go`](../../bootstrap/resume_test.go#L16-L108).

**Failure signals**

Missing refresh token, corrupted key pair, certificate common-name mismatch, auth sign-in rejection, or expired key. Long-running refresh while a relay carries traffic remains unvalidated.

### 5. Peer presence and wake

**Probe**

```bash
go run ./cmd/linkmyphone peer-probe
```

The target is a flag value, not a positional argument:

```bash
go run ./cmd/linkmyphone peer-probe --target "linked Android device"
go run ./cmd/linkmyphone peer-probe --wake-timeout 60s
```

**Historical result**

The probe reused persistent auth, refreshed trust, connected the account relay, checked presence, and used signed `Dispatcher/Wake` when the target was absent. The pre-wake presence flush was added after earlier ordering work: `SendConnectedAsync` is sent and its Hub Completion awaited before `Dispatcher/Wake`.

**Expected signals**

Existing partner presence avoids wake. An absent target produces a wake request and later `OnPartnerConnected` or peer traffic. The CLI reports status without dumping wake JWTs or request data.

**Failure signals and limits**

Missing target or trust identity, DCG wake HTTP error, Hub Completion rejection, wake timeout, or no partner traffic after dispatch. A successful HTTP wake response does not prove that the peer connected. The baseline reports peer presence/wake probe work and pre-wake ordering; continuous wake recovery after sleep or network loss remains open.

### 6. PLATFORM SessionValidation

**Probe**

```bash
go run ./cmd/linkmyphone session-probe
```

**Historical result**

After trace-context correction, an S23 accepted a Linux PLATFORM `/SessionValidation` request and returned `/internal/response` carrying `SessionValidation`, `PersistentMessageChannel`, and `NanoTransportPreference`. Observed versions were 14 and 3.

**Request shape**

```text
route: /SessionValidation
ms-content-type: application/x-binary
_rejectionVersion: 1
_requestId: <numeric ID>
protobuf: PlatformCapabilities = [SessionValidation]
```

The probe matches `_originalRequestId`, prints capability/version metadata, and does not print raw platform payloads. The response must include `SessionValidation`; peer rejection or unrelated response is reported separately. Source and boundary tests: [`bootstrap/session.go`](../../bootstrap/session.go#L54-L120) and [`bootstrap/session_test.go`](../../bootstrap/session_test.go#L46-L179).

**Failure signals and limit**

Peer rejection header, empty payload, missing capability, wrong source, wrong transport type, mismatched request ID, malformed platform envelope, or timeout. One S23 run reported success; capability versions are observations, not a support matrix.

### 7. Context Publish and STATUS/CONTENT

**Probe**

```bash
go run ./cmd/linkmyphone session-probe --context-probe
```

Safe explicit-content probe:

```bash
go run ./cmd/linkmyphone session-probe --context-probe --context-timeout 15s --context-text "linkmyphone probe"
```

**Historical result**

The S23 accepted tag-9 `/Context/Publish` with `PubSubPayload.Data` carrying a `CLIPBOARD_CHANGE`. It requested `GET /clipboard STATUS` with the same clipboard correlation. The probe answered `Success + FEATURE_ON`; a later run requested `CONTENT` directly. With explicitly supplied non-sensitive text, a successful `text/plain` response appeared on the S23 and was pasteable on the phone. The CLI reported byte count, not text.

**Expected sequence**

```text
Context/Publish(tag=9, Data=CLIPBOARD_CHANGE[cid])
  <- /DeviceResourceManager GET /clipboard STATUS[cid]
  -> /internal/response Success + FEATURE_ON[cid]
  <- /DeviceResourceManager GET /clipboard CONTENT[cid]
  -> /internal/response Success + TEXT_PLAIN[cid]
```

A finite probe without `--context-text` declines CONTENT with `ResourceHandlerNotRegistered`, so it does not intentionally send clipboard content. A timeout after DCG acknowledgement is recorded as an observation rather than automatically classified as a transport failure. Implementation: [`bootstrap/context.go`](../../bootstrap/context.go#L75-L300). Route and message codecs are tested in [`protocol/platform/message_test.go`](../../protocol/platform/message_test.go#L82-L108) and [`protocol/clipboard/codec_test.go`](../../protocol/clipboard/codec_test.go#L25-L121).

**Failure signals and limits**

No Android reaction before context timeout, wrong PubSub direction, wrong tag, malformed MSAEP or clipboard message, missing request ID, wrong correlation, unsupported resource operation, or peer rejection. No raw request payload or clipboard text belongs in a report. The observed negotiation sequence is not mandatory for every peer.

### 8. Continuous native clipboard sync

**Historical command**

```bash
go run ./cmd/linkmyphone clipboard-sync
```

The modular path is preferred:

```bash
go run ./cmd/linkmyphone feature create --enabled linkmyphone.clipboard
go run ./cmd/linkmyphone run
```

**Historical result**

The baseline reports bidirectional text synchronization on Wayland with an S23. It also reports:

- the `wl-copy` fork and pipe-latency bug was fixed;
- phone-to-Linux logging became immediate;
- reflected clipboard echo was eliminated;
- outbound text snapshots stayed tied to correlation IDs;
- the origin barrier closed the polling versus remote-write race;
- `/Context/Publish` work moved to a publisher worker;
- rapid local changes coalesced to the newest pending publication;
- `wl-paste --watch` emitted immediate single publications for consecutive copies;
- nil debounce emitted exactly one empty publication for a genuine clear;
- phone-to-Linux writes remained non-echoing;
- Ctrl+C shut down the watcher and module cleanly without entering polling fallback.

**Expected and failure signals**

Diagnostics name observer mode and direction, byte counts, and shortened IDs only. Wayland selects `wl-clipboard` and event watching; X11 falls back to polling with default 500 ms interval. An empty Wayland selection is valid empty text.

No native provider, watcher startup failure, compositor lacking required data-control protocol, malformed helper frame, clipboard read/write failure, stale generation, or relay response timeout indicates failure. Native helper boundaries are tested in [`clipboard/native_watch_test.go`](../../clipboard/native_watch_test.go#L12-L153), and backend selection/empty-selection behavior in [`clipboard/native_test.go`](../../clipboard/native_test.go#L9-L138).

Wayland/S23 was live validated. X11, other compositors, and other phones were not.

### 9. Modular runtime and live feature CRUD

**Historical sequence**

```bash
go run ./cmd/linkmyphone feature create --enabled linkmyphone.clipboard
go run ./cmd/linkmyphone run
```

While `run` remains active, the baseline says these operations completed without restarting the process:

```bash
go run ./cmd/linkmyphone feature list
go run ./cmd/linkmyphone feature get linkmyphone.clipboard
go run ./cmd/linkmyphone feature disable linkmyphone.clipboard
go run ./cmd/linkmyphone feature enable linkmyphone.clipboard
go run ./cmd/linkmyphone feature update --config '{"poll_interval_ms":250,"request_timeout_ms":10000,"publish_initial":false}' linkmyphone.clipboard
go run ./cmd/linkmyphone feature delete linkmyphone.clipboard
go run ./cmd/linkmyphone feature create --enabled linkmyphone.clipboard
```

Flags precede the positional feature ID in update and create forms. Historical states were Ready to Stopped on disable, Ready again on enable, stop/restart and epoch increase on configuration update, live revocation on delete, and a fresh Ready module after create. Delete plus create starts a new epoch at 1; restarting the same registry entry increments the existing epoch. The shared Phone Link session remained alive. Historical socket path: `/run/user/<uid>/linkmyphone/<store-hash>.sock`.

**Expected signals and historical limit**

`list` and `get` report live state while the daemon owns the socket. Commands during `activating` return `runtime is starting; retry the feature command` instead of changing desired state offline. Once ready, commands return live state. Source paths are [`cmd/linkmyphone/runtime_control.go`](../../cmd/linkmyphone/runtime_control.go), [`runtime/kernel/`](../../runtime/kernel/), and [`runtime/controlplane/`](../../runtime/controlplane/). `run --request-timeout` controls only the host's PLATFORM `/SessionValidation` wait. Clipboard operations use the separate module `request_timeout_ms` configuration.

The baseline reports an end-to-end S23 run, targeted stress tests, full `go test ./...`, and full `go test -race ./...` after lifecycle, router, generation, socket, and reconcile changes. This is a historical report, not a fresh run, and does not establish recovery after losing the cloud session.

### 10. systemd user service

**Historical command**

```bash
go run ./cmd/linkmyphone service install
```

**Historical result**

On CachyOS/Wayland, installation copied the current binary and unit, `Type=notify` held startup until runtime Ready, the service became `active (running)`, the control socket was reachable, and the watcher ran inside the service cgroup. Historical restart recreated the watcher and capabilities. Explicit stop removed only the Phone Link watcher while unrelated watchers remained; start recreated the runtime. Logout stopped cleanly; login started the enabled unit automatically with new `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`; live CRUD remained available. An empty pre-existing Wayland selection did not block Ready. Feature commands during `activating` returned the explicit retry response.

**Expected and failure signals**

`service status` shows `active (running)` after readiness. Logs include `LinkMyPhone runtime ready`, and module shutdown reports `module stopped`. Generated unit and readiness paths are under [`packaging/systemd/`](../../packaging/systemd/) and [`runtime/systemdnotify/`](../../runtime/systemdnotify/).

Missing graphical-session environment, no clipboard provider, unit installation failure, readiness timeout, cgroup child cleanup failure, stale control socket, or a service that remains activating indicates failure. No raw journal is tracked.

The complete user-service lifecycle was reported live on one Wayland desktop. Other desktop/session managers and init systems remain unvalidated.

## Historical automated test reports

The baseline README reports these prior checks after corresponding feature work:

- pre-modular continuous clipboard path: `go test ./...` and `go test -race ./...`;
- modular lifecycle, router, generation, Unix socket, and live reconcile changes: targeted stress tests plus `go test ./...` and `go test -race ./...`;
- event-driven Wayland watcher: targeted tests repeated 50 times and targeted race tests repeated 50 times, then full `go test ./...` and `go test -race ./...`;
- systemd packaging and empty-clipboard fix: targeted tests repeated 50 times and targeted race tests repeated 50 times, then full `go test ./...` and `go test -race ./...`.

These are reports of prior runs, not newly captured results. Their source/test pointers are in [findings](findings.md) and the protocol references. Record a new run's commit, environment, command, and outcome separately rather than treating these assertions as current proof.

## Absent artifacts and provenance limits

The repository does not contain:

- the Windows decompiled source files that informed source-confirmed comments;
- raw Microsoft authentication responses or WAM traces;
- raw WebSocket frames showing handshake coalescing;
- raw Hub Completion, DCG ACK, wake, or trace logs;
- a packet capture of S23 SessionValidation or clipboard exchanges;
- systemd journal output from historical service runs;
- a complete Linux environment manifest for the historical Wayland run;
- phone model coverage beyond the reported S23;
- evidence for X11 or non-Wayland clipboard watching;
- evidence of successful long-running reconnect, peer wake recovery, or token refresh.

The checkable record is current source, focused tests, dated commits, PR #1 body, and historical prose in the main README. No external decompiled-source location or raw live artifact is cited because none is tracked.

## Open limitations

Microsoft services provide account tokens, trust discovery, relay and wake. Recovery has been tested with one S23 on Wayland; token expiry, extended operation, other phones and authenticated X11 runs still need testing. MSAEP version 1.1, clipboard tag 9 and capability versions 14 and 3 describe the recorded protocol exchanges. See the dated results above for the tested setup.
