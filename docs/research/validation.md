# Validation and safe probes

[Documentation index](../README.md)

The live results below were reported in the [baseline README at `e51728b`](https://github.com/YMGPwcca/phonelink-linux/blob/e51728b12a148061c8076c67200ee4a8f366e423/README.md), before this documentation restructure. Raw live logs, packet captures, systemd journals, and phone clipboard payloads were not committed. [Development history](history.md) establishes commit order; source and tests establish local contracts. Neither replaces missing production artifacts.

Command examples, feature IDs, paths, and expected log labels below use the current LinkMyPhone names, including in historical sequences. They are updated equivalents, not verbatim transcripts or evidence of a new live validation of the renamed client. Follow the linked baseline for the original names.

The current rich clipboard results are recorded below separately from the baseline. The older stage sections retain their historical scope.

## Read the result before running a probe

| Stage | Recorded outcome | Boundary still open |
| --- | --- | --- |
| Microsoft device-code login | Migrated CrossDevice scope and public client reached the device-code endpoint; login succeeded. | Service and account availability are not guaranteed now. |
| Identity, enrollment, trust, relay | Identity and trust enrollment succeeded; linked peers, assigned shard, and `relayhub/` reached `OnConnected`. | Reconnect and token refresh during an active relay remain unvalidated. |
| Peer wake | S23 presence/wake path was reported, including pre-wake presence flush. | Repeated wake recovery after sleep or network loss remains open. |
| SessionValidation | One S23 run accepted PLATFORM `/SessionValidation`; response included capability versions 14 and 3. | These versions are one observation, not a support matrix. |
| Context Publish | Tag-9 publication reached the S23; STATUS then CONTENT was observed, and explicit non-sensitive text was pasteable. | Negotiation is not mandatory for every peer; raw exchange is absent. |
| Continuous clipboard | Baseline text sync plus current HTML/image reports on CachyOS/Wayland with an S23; see the dated results below. | Live authenticated X11, other compositors, other phones, and durable reconnect are unvalidated. |
| Modular CRUD and service | Live feature CRUD and systemd lifecycle were reported on the same Wayland/S23 setup. | Other session managers and init systems are unvalidated. |

## Evidence classes and environment

| Class | Meaning |
| --- | --- |
| Current owner report | A new operator report establishes visible behavior on the reported devices; absent payload files do not permit pixel or byte comparison. |
| Historical live report | The baseline README says a prior operator saw the result in a real environment. No raw artifact is tracked. |
| Automated test report | The baseline README reports that a prior branch run completed a named command; this is historical evidence, not a fresh result. |
| Current source contract | A source or focused test path provides a checkable local contract, independent of remote availability. |
| Unvalidated | No tracked live report establishes the behavior for the stated environment or scenario. |

The historical live environment was a Linux desktop running CachyOS with Wayland and a linked Samsung S23. The README does not identify a complete hardware, compositor, package, network, or service-version manifest. Windows WAM behavior came from source findings, not this Linux probe. X11 and other phone models were not live validated in the tracked record.

## Clipboard validation (2026-10-03)

Environment reported by the owner: CachyOS/Wayland, `wl-clipboard`, a Samsung Galaxy S23 running Link to Windows, and Windows Phone Link for comparison. The new executable reused the old `~/.config/phonelink-linux/state.json` through an explicit state flag. HTML dual offers had GTK4/Python GI available. Exact Android, Windows, Link to Windows and compositor versions were not supplied. Dates here use the owner's Asia/Ho_Chi_Minh timezone.

| Check | Reported result | Evidence boundary |
| --- | --- | --- |
| Runtime and content negotiation | PLATFORM session ready, FEATURE_ON synchronized, module `0.2.0` ready; text/HTML/image read/write/bidirectional capabilities active. | Readiness establishes negotiated runtime state, not every paste target. |
| Received HTML and text | Runtime applied `text/html` and `text/plain;charset=utf-8`; rich clipboard support accepted for this tested setup. | Received HTML is recorded; a controlled Windows-versus-Linux formatting comparison and arbitrary RTF support are not established. |
| Image transfer in both directions | Phone → Linux and Linux → phone image pastes worked. JPEG/BMP normalization was added; the owner confirmed phone-image transfer after `2670f26`. | Automated tests establish codec variants and budgets; the live sample set does not cover every codec variant or app. |
| Incoming resize regression | Before the fix, an earlier phone sample yielded 1523×2706 on Windows but 471×836 on Linux. After `7f999f3`, the same new phone copy pasted as **1572×2096 on both PCs**. | The before/after dimensions refer to different reported samples. The matching new sample verifies dimension preservation, not byte or pixel identity. |
| Outbound same-source comparison | Linux → phone: **583×1036**, displayed **1.05 MB**. Windows → phone: **310×551**, displayed **330 KB**. Owner judged Linux better for this comparison card. | Linux retained about 3.54 times the pixels in this case. Sizes were rounded UI values; exact payload files and byte counts were not supplied. No universal quality ranking is inferred. |
| Automated verification | [Run 37098891014](https://github.com/YMGPwcca/linkmyphone/actions/runs/37098891014) at `7f999f3` passed vet, build, full race suite and real X11 text/HTML/image MIME transfers. | Xvfb is local desktop integration, not a live authenticated Android/X11 check. |

Rich text & HTML clipboard and Image clipboard are **Supported** within the documented provider, format and size limits. The receive conversion preserves dimensions; only outbound PNG preparation applies **1048576 bytes (1 MiB)**. A display rounded to 1.05 MB is not by itself evidence of exceeding that exact limit. Linux and Windows use different outbound resampling and size-selection policies.

CI subsequently changed at `98a9e5c` to run only on main updates, including merges and direct pushes. Historical successful feature-branch runs remain valid evidence of those commits; future feature pushes or manual dispatch do not trigger the workflow.

No original clipboard image files, pixel comparisons or full live transcripts are committed. Live recovery/soak validation, other phones and live authenticated X11 remain open. Automatic recovery and token renewal are now implemented; see [session resilience](#session-resilience-2026-10-03).

## Session resilience (2026-10-03)

Implementation on `feature/session-resilience` adds in-process session supervision to `run` and `clipboard-sync`, relay keepalive/deadlines, peer-disconnect and wall-clock gap detection, scheduled token renewal, credential checkpoints and feature-generation handoff. [Source observations and ownership](../architecture/session-resilience.md) document the supplied Android/Windows evidence and Linux policy choices.

| Check | Current result | Evidence boundary |
| --- | --- | --- |
| `go vet ./...` and CLI build | Passed with Go 1.24.4 on Linux. | Compilation/static checks, not authenticated operation. |
| Auth/bootstrap/phonehost/transport/protocol fault tests with race detector | Passed. | Includes local HTTP/WebSocket faults, cancellation, silence timeout, retry/throttling, renewal scheduling and rotated-credential persistence. |
| Controller handoff and reloading committed desired state | Passed with race detector. | In-memory handler tests; replacement respects disabled state/config and CRUD can enable it afterwards. |
| Full `go test -race ./...` | Blocked: 11 existing tests fail with `socket: operation not permitted` in this workspace. The same 11 fail on the untouched baseline. | Three feature-command tests, seven Unix control-socket tests and one systemd Unix-datagram test need a Linux environment that permits AF_UNIX. No passing full-suite result is claimed. |
| Full race run excluding exactly those 11 blocked tests | Passed across all packages. | Test code and CI were not changed to skip them; Unix-socket integration remains unverified in this run. |
| Production network/phone loss, actual suspend and multi-hour renewal | Not run. | No authenticated Microsoft session, phone recovery report, real X11 MIME integration run or soak result was supplied for this change. |

The skip list for this environment was `TestFeatureCommandCRUDForClipboard`, `TestFeatureCommandCanDisableAndDeleteStaleRecord`, `TestFeatureCommandDoesNotFallbackOfflineWhenRuntimeRejectsMutation`, `TestServerRoundTripAndSocketPermissions`, `TestCallUnavailable`, `TestListenReplacesStaleNonSocketPath`, `TestSwitchHandlerTransitionsWithoutReplacingSocket`, `TestServerCloseStopsServeWithoutContextCancellation`, `TestHandlerPanicReturnsFailureAndServerSurvives`, `TestSocketPathRemainsShortForDeepFeatureStore`, and `TestReadySendsSystemdDatagram`. Other tests in these packages, including the new handoff/controller tests, ran.

**Session recovery & refresh remains Partial** because the implementation has local regression coverage while real cloud recovery and multi-hour reliability are still unvalidated. Follow [live checks](../operations/session-recovery.md#live-checks) and record device, desktop and network conditions before promoting it to Supported. CI remains limited to main pushes; this feature branch does not trigger it.

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

Microsoft cloud services remain required. Finite bootstrap, wake, SessionValidation, Context Publish, and clipboard probes do not establish a durable connection. Automatic recovery, wake/re-presence and token renewal now have local regression coverage. Live cloud recovery and multi-hour reliability remain unvalidated. Protocol constants such as MSAEP version 1.1, clipboard tag 9, enum values, and observed capability versions 14 and 3 must not be read as a general compatibility or phone-support matrix.
