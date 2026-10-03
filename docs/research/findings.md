# Research findings

[Documentation index](../README.md)

The [baseline README at `e51728b`](https://github.com/YMGPwcca/phonelink-linux/blob/e51728b12a148061c8076c67200ee4a8f366e423/README.md) reports the production observations summarized here. Source and tests establish the Linux implementation's behavior. The original Windows source corpus, raw production frames, live logs, and phone payloads are not tracked, so historical observations cannot be independently replayed from this checkout. [Research method](method.md) describes that provenance boundary.

## Findings at a glance

| Group | Result |
| --- | --- |
| Authentication | WAM and Linux device-code scopes are distinct; auth identity, trust identity, and account trust are separate; persistence boundaries differ between CLI and library first-run paths. |
| Transport | Binary and coalesced SignalR handshakes, DCG and Hub session IDs, Hub transport enum values, Hub Completion versus peer ACK, pre-wake presence, and trace shape each became explicit compatibility boundaries. |
| Session and clipboard | Session capability versions were observed on one S23; PubSub direction is asymmetric; STATUS is optional in later runs; correlation snapshots and generation tombstones prevent stale content. |
| Ordering and native clipboard | Receive handling continues while publication waits; a 3-second echo barrier handles remote-write races; Wayland writes avoid inherited pipe blocking and debounce transient nil selection. |
| Runtime and service | Shared host and feature lifecycle are separate; capability registration and Ready use separate locks; live CRUD reserves the startup socket; systemd readiness follows the graphical session. |
| Rich clipboard | Text/HTML/images are supported; incoming image dimensions are preserved, and the 1 MiB PNG budget applies only to outbound preparation. Live owner reports and X11 integration tests are recorded separately. |
| Limits | Long-running reconnect, repeated wake/re-presence, active-session token refresh, live authenticated X11, other compositors, other phones, and Windows WAM execution remain unvalidated in the tracked record. |

Each result keeps its evidence trail in an expandable section. Findings and uncertainty stay visible; open the evidence when tracing a source, test, or corrective commit.

## Authentication and enrollment

### WAM and device-code scopes are different

Historical Windows source notes describe CrossDevice acquiring its Microsoft token through WAM: provider `https://login.microsoft.com`, authority `consumers`, public CrossDevice client ID, and suffix `&api-version=2.0&clientid=<app-id>`. That suffix is specific to WAM. Linux uses a standard device-code request with `https://dcg.microsoft.com/DCG.ReadWrite offline_access`.

<details>
<summary>Scope constants, device-code tests, and historical sign-in</summary>

Constants and modeled parameters are in [`auth/msa/config.go`](../../auth/msa/config.go#L5-L25), [`auth/msa/devicecode.go`](../../auth/msa/devicecode.go#L21-L24), and [`auth/dcgauth/constants.go`](../../auth/dcgauth/constants.go#L21-L46). [`auth/msa/devicecode_test.go`](../../auth/msa/devicecode_test.go#L13-L123) checks request and polling behavior. The baseline reports HTTP 200 from the Linux device-code probe and successful sign-in.

</details>

The same notes describe YPP using `GetTokenSilentlyAsync` and forwarding `UserInteractionRequired` or `AccountSwitch` without an interactive fallback in that layer. This client does not execute Windows WAM; its interactive sign-in is the device-code flow.

### Identity, trust identity, and account trust are separate

A local DCG auth identity has a random UUID and an ECDSA P-384 self-signed certificate whose common name is that UUID. Its nonce JWT uses ES384. Enrollment registers a separate `trust_<dcg-client-id>` certificate under `SelfSigned`; signed wake uses that trust identity. The returned account certificate becomes A2D trust, while linked peer certificates become AsyncD2D trust.

<details>
<summary>Identity, enrollment, and trust sources and tests</summary>

Identity creation and JWT claims are implemented in [`identity.go`](../../auth/dcgauth/identity.go#L31-L150) and checked by [`identity_test.go`](../../auth/dcgauth/identity_test.go#L14-L75). Certificate registration is in [`enroll.go`](../../auth/dcgauth/enroll.go#L61-L167) and [`enroll_test.go`](../../auth/dcgauth/enroll_test.go#L12-L76). Trust mapping and certificate preference are in [`trust.go`](../../auth/dcgauth/trust.go#L41-L148), [`devices.go`](../../auth/dcgauth/devices.go#L79-L124), and [`trust_test.go`](../../auth/dcgauth/trust_test.go#L9-L73). The production bootstrap report says the service accepted identity creation and enrollment.

</details>

### Persistence protects the identity before relay setup

The public bootstrap probe saves new keys and refresh credentials immediately after enrollment, synchronizes trust, saves again, and opens the account relay. A trust or relay failure after the first save can be investigated without creating another identity. The library `BootstrapFirstRun` has a different boundary: it synchronizes trust before its state save, then opens the relay.

<details>
<summary>Persistence order, stored fields, and resume evidence</summary>

The CLI order is visible in [`main.go`](../../cmd/linkmyphone/main.go#L160-L264); the library order is in [`first_run.go`](../../bootstrap/first_run.go#L47-L165). State carries both key pairs, refresh credentials, the services token, enrollment material, trust relationships, and a stable logical-device ID. Resume refreshes the Microsoft token and calls DCG `SignIn`, not `CreateIdentity`.

[`resume.go`](../../bootstrap/resume.go#L20-L80) and [`auth/state/store.go`](../../auth/state/store.go#L18-L207) implement the contracts. The library's staged persistence and resume behavior are covered by [`first_run_test.go`](../../bootstrap/first_run_test.go#L22-L141), [`resume_test.go`](../../bootstrap/resume_test.go#L16-L108), and [`store_test.go`](../../auth/state/store_test.go#L12-L96). The baseline reports production state persistence before later cloud stages.

</details>

## Transport findings

### Binary and coalesced SignalR handshake

The production bootstrap report says SignalR returned its JSON handshake inside a binary WebSocket message. The same message could carry the first Hub record after the handshake's record separator. Accepting text-only handshakes or discarding bytes after the separator would lose valid service traffic.

<details>
<summary>Handshake implementation, regression tests, and correction</summary>

[`transport/signalr/client.go`](../../transport/signalr/client.go#L97-L137) accepts text or binary handshake messages and preserves trailing bytes for the next read. [`client_test.go`](../../transport/signalr/client_test.go#L21-L111) covers binary and coalesced cases. Commit [`e6cb89f`](https://github.com/YMGPwcca/phonelink-linux/commit/e6cb89f0d1761e44847294c64876662113df335d) records the correction.

</details>

### DCG SessionId and Hub connectionSessionId are independent

When a caller omits a session ID, the relay generates and retains a stable DCG `SessionId` UUID per target for its lifetime. A caller can supply a non-empty ID explicitly. The DCG ID appears in fragment and ACK properties. Optional Hub `connectionSessionId` belongs to separately modeled session-based invocations; copying it into DCG `Properties["SessionId"]` confuses two contracts.

<details>
<summary>Session ID ownership, stability tests, and correction</summary>

The relay creates and retains the per-target IDs in [`client.go`](../../transport/relay/client.go#L298-L335) and [`client.go`](../../transport/relay/client.go#L647-L666). [`invocation.go`](../../protocol/signalr/invocation.go#L62-L97) models the Hub argument separately. Tests cover stability and invocation shape in [`relay/client_test.go`](../../transport/relay/client_test.go#L331-L354) and [`invocation_test.go`](../../protocol/signalr/invocation_test.go#L78-L123). Commit [`69acae8`](https://github.com/YMGPwcca/phonelink-linux/commit/69acae8e5eea909a8dd8fefaf728cd0389d156cc) records the separation.

</details>

### Hub transport enum differs from the protobuf enum

Hub Relay uses `App=0`, `Platform=1`, and `Unknown=2`. The separate protobuf enum has `Unspecified=0`, `App=1`, and `Platform=2`. Using protobuf numbers in Hub packets was the wrong mapping. Both send and receive now use Hub values in `MultiplexPacket.Properties["MessageType"]`.

<details>
<summary>Enum definitions, wire-value test, and correction</summary>

Definitions, packet encoding, and the wire-value test are in [`constants.go`](../../protocol/dcg/constants.go#L16-L30), [`fragment.go`](../../protocol/dcg/fragment.go#L67-L81), and [`packet_test.go`](../../protocol/dcg/packet_test.go#L48-L58). Commit [`d737b21`](https://github.com/YMGPwcca/phonelink-linux/commit/d737b21442b9f2ac67afdacb3008930a540679f3) corrects the mapping. The baseline reports this correction was required during the production probe.

</details>

### Hub Completion and peer DCG ACK are different results

Historical Windows source notes describe Hub Relay operations using `InvokeAsync`. The relay therefore tracks Hub `Completion` as well as the peer's DCG ACK for each fragment. A Hub rejection, a Hub-accepted send with no peer ACK, and a send with neither result are different failures. A missing ACK alone does not identify the remote cause.

<details>
<summary>Completion waiters, fake-Hub tests, and correction</summary>

Separate waiters are implemented in [`relay/client.go`](../../transport/relay/client.go#L337-L420) and [`relay/client.go`](../../transport/relay/client.go#L477-L541). [`completion.go`](../../protocol/signalr/completion.go#L8-L66) parses the Hub result. Fake-Hub checks in [`client_test.go`](../../transport/relay/client_test.go#L509-L644) exercise rejection, flush completion, and send behavior. Commit [`002ea31`](https://github.com/YMGPwcca/phonelink-linux/commit/002ea31398ea156221f1fb85719b6dfcd83c47a9) introduced completion tracking.

</details>

### Pre-wake presence flush

Before `Dispatcher/Wake`, the client sends `SendConnectedAsync(target)` and waits for its Hub completion. Reciprocal presence only after `OnPartnerConnected` was insufficient for the recorded wake path; reciprocal presence remains after the initial flush.

<details>
<summary>Presence flush implementation and historical wake report</summary>

[`FlushPartner`](../../transport/relay/client.go#L198-L247) performs the completion-waited send. [`bootstrap/peer.go`](../../bootstrap/peer.go#L1-L119) integrates presence checks and wake. Commit [`abb99ca`](https://github.com/YMGPwcca/phonelink-linux/commit/abb99ca25f927e12367ef68b619ddba2fe8ec79d) records the fix. The baseline and [PR #1](https://github.com/YMGPwcca/phonelink-linux/pull/1) describe Windows pre-wake ordering. Later production success was reported, but repeated wake recovery during a long-lived session remains open.

</details>

### Trace context shape

Hub Relay trace maps need non-null `TraceId`, `ParentId`, and `TraceState`. The implementation generates a 32-character trace ID, a 16-character parent ID, zero flags, and a non-nil empty trace-state map. The earlier empty trace object was incompatible with the recorded receiver path.

<details>
<summary>Trace generation, normalization tests, and correction</summary>

Trace generation and normalization are in [`trace.go`](../../protocol/signalr/trace.go#L9-L52), [`invocation.go`](../../protocol/signalr/invocation.go#L216-L264), and [`trace_test.go`](../../protocol/signalr/trace_test.go#L5-L30). Commit [`4652bb4`](https://github.com/YMGPwcca/phonelink-linux/commit/4652bb4729b187e3f9efae4d33ba0bc24bcfd080) records the correction. The baseline reports that the S23 accepted SessionValidation after the trace shape was fixed.

</details>

Failure before DCG processing, and therefore before ACK generation, is the inferred explanation from the source-described trace conversion and missing ACK. No raw receiver log is tracked to establish that internal failure directly.

## Session and clipboard findings

### Observed SessionValidation versions

The finite probe advertises only `SessionValidation`. The historical S23 response also included `PersistentMessageChannel` and `NanoTransportPreference`, with versions 14 and 3. Those numbers describe one response; they are not a phone support matrix or proof that Linux implements optional transports.

<details>
<summary>Session probe sources, tests, and version provenance</summary>

The capability codec and request correlation are in [`codec.go`](../../protocol/sessionvalidation/codec.go#L8-L155), [`bootstrap/session.go`](../../bootstrap/session.go#L33-L120), and [`session_test.go`](../../bootstrap/session_test.go#L46-L179). Commit [`7bc660e`](https://github.com/YMGPwcca/phonelink-linux/commit/7bc660e77a09f85d87b81329d3883837f0221f98) introduced the probe. Version observations come from the baseline report.

</details>

### PubSub direction is asymmetric

For clipboard tag 9, PC-to-phone publication puts serialized `CLIPBOARD_CHANGE` in `PubSubPayload.Data`. Phone-to-PC publication uses `PubSubPayload.Additional`. The phone-publication parser rejects payloads that supply only `Data`.

<details>
<summary>Directional PubSub parser, tests, and correction</summary>

The directional model is in [`pubsub.go`](../../protocol/clipboard/pubsub.go#L10-L31), [`pubsub_test.go`](../../protocol/clipboard/pubsub_test.go#L5-L38), and [`clipboard/client.go`](../../clipboard/client.go#L554-L580). Commit [`a4ffac0`](https://github.com/YMGPwcca/phonelink-linux/commit/a4ffac0bc6a9aa6b32adfa1473a4e4d72cada257) records the finding.

</details>

### STATUS negotiation and direct CONTENT

After the Linux tag-9 publication, the recorded S23 run requested `GET /clipboard STATUS` with the advertised clipboard correlation. The probe answered `Success + FEATURE_ON`, then waited for CONTENT. A later run requested CONTENT directly. In the other direction, the normal client immediately pulls CONTENT when it receives a phone publication; it does not issue STATUS first.

<details>
<summary>Finite probe stages, explicit-content tests, and reports</summary>

The finite probe is implemented in [`context.go`](../../bootstrap/context.go#L49-L58) and [`context.go`](../../bootstrap/context.go#L180-L297); the immediate phone CONTENT path is in [`clipboard/client.go`](../../clipboard/client.go#L345-L359). Commits [`4b4c093`](https://github.com/YMGPwcca/phonelink-linux/commit/4b4c09312d6974f9b0df8c77141349b8d973d7cf), [`ee33183`](https://github.com/YMGPwcca/phonelink-linux/commit/ee3318368e1f8e73b683b2912ab59a9ef16d39d5), and [`77844a5`](https://github.com/YMGPwcca/phonelink-linux/commit/77844a5b1148c14ef585a396c2af55563beff90a) record publication, negotiation, and explicit-content stages.

The baseline reports that explicitly supplied, non-sensitive probe text reached the S23 clipboard and could be pasted. Without explicit text, the probe declines CONTENT instead of returning real clipboard data. [`context_test.go`](../../bootstrap/context_test.go#L15-L547) covers STATUS followed by CONTENT, status-only timeout, rejection, timeout-as-observation, explicit text, and the 4096-byte limit. The observed negotiation sequence is not mandatory for every peer.

</details>

### Correlation snapshots and tombstones prevent stale content

A PC publication stores exact text under the clipboard correlation before sending the change notification. A later CONTENT request receives that snapshot even if local clipboard text changed. Snapshots expire after two minutes or bounded eviction. A known retired or superseded correlation returns `INVALID_CONTENT` rather than unrelated current text. A newer remote generation tombstones older versioned local snapshots. Retirement history is bounded to 256 IDs; once an old ID is forgotten, the unknown-ID fallback can apply.

<details>
<summary>Snapshot boundaries, generation tests, and corrections</summary>

Snapshot, expiry, and tombstone code is in [`client.go`](../../clipboard/client.go#L205-L343), [`client.go`](../../clipboard/client.go#L361-L405), and [`client.go`](../../clipboard/client.go#L688-L748). Boundary tests are in [`client_test.go`](../../clipboard/client_test.go#L220-L278), [`client_test.go`](../../clipboard/client_test.go#L611-L682), [`retired_snapshot_test.go`](../../clipboard/retired_snapshot_test.go#L14-L71), and [`generation_test.go`](../../clipboard/generation_test.go#L39-L108). Commits [`8da85c7`](https://github.com/YMGPwcca/phonelink-linux/commit/8da85c778d5f072a92336c3e4daa93606551b035), [`a37e286`](https://github.com/YMGPwcca/phonelink-linux/commit/a37e286c85e451ed4bbda59074d62f730b4eb120), and [`5d0d5c1`](https://github.com/YMGPwcca/phonelink-linux/commit/5d0d5c154fbfdb0fb18c988ca75a8a676855b82f) record the corrections.

</details>

## Ordering, echo, and native clipboard behavior

### Receive handling continues while publication waits

Outbound Context Publish runs on a separate publisher worker. Waiting for DCG acknowledgement does not block receive handling or phone-to-Linux logging. Rapid local changes coalesce to the newest queued value while one publication is in flight. Incoming phone pulls are cancellable, a newer generation supersedes an older pull, and the incoming resource-request queue is bounded; overflow is an explicit error.

<details>
<summary>Publisher ownership, queue tests, and ordering corrections</summary>

The feature publisher is in [`features/clipboard/sync.go`](../../features/clipboard/sync.go). Protocol workers are in [`clipboard/client.go`](../../clipboard/client.go#L114-L142) and [`clipboard/client.go`](../../clipboard/client.go#L583-L659), with queue and late-response tests in [`client_test.go`](../../clipboard/client_test.go#L446-L568). Commits [`9cf0521`](https://github.com/YMGPwcca/phonelink-linux/commit/9cf052198995621a21d9658fbd397be1266bdcc1), [`a37e286`](https://github.com/YMGPwcca/phonelink-linux/commit/a37e286c85e451ed4bbda59074d62f730b4eb120), and [`6e50c69`](https://github.com/YMGPwcca/phonelink-linux/commit/6e50c69f29fb853cbb776e4c62c9fba5f2bc9b9c) record ordering changes and immediate propagation of relay read-loop failure.

</details>

### Reflected updates and the remote-write race

The client skips a native write when phone CONTENT exactly matches current local text. Before a remote write begins, the feature installs an origin barrier suppressing previous and incoming normalized text hashes for three seconds. Echo comparison normalizes CRLF/LF and one terminal newline while preserving original bytes for publication.

<details>
<summary>Equality and origin-barrier tests and Wayland report</summary>

The equality check is in [`clipboard/client.go`](../../clipboard/client.go#L407-L445); origin and echo handling are in [`features/clipboard/sync.go`](../../features/clipboard/sync.go). Tests are in [`client_test.go`](../../clipboard/client_test.go#L295-L372) and [`sync_test.go`](../../features/clipboard/sync_test.go). Commits [`9a039c6`](https://github.com/YMGPwcca/phonelink-linux/commit/9a039c634f87d4ff05fff0abfa31e100e040341e) and [`9b55277`](https://github.com/YMGPwcca/phonelink-linux/commit/9b552776b996f91ab9285305cd10f6feb912b7dd) record the fixes. The historical Wayland check reports reflected echo was eliminated. X11 has no tracked live validation.

</details>

### Wayland fork and nil-clear behavior

`wl-copy` forks to keep serving a selection. Capturing stdout or stderr can leave the parent waiting on pipes inherited by that long-lived owner. The native write path uses direct execution without captured output pipes.

`wl-paste --watch` invokes a hidden helper in the same executable to frame clipboard events. A 100 ms debounce distinguishes transient `CLIPBOARD_STATE=nil` ownership gaps from genuine clears. A following data or sensitive-state event cancels a transient clear; a genuine clear publishes one empty value. This state handling is not sensitive-clipboard filtering. The watcher has its own process group so Ctrl+C can reach module lifecycle before watcher teardown is mistaken for a failure. X11 and failed or unavailable Wayland watching use polling fallback.

<details>
<summary>Native watcher sources, helper tests, and validation report</summary>

Native paths and helper tests are in [`native.go`](../../clipboard/native.go#L62-L86), [`native_watch.go`](../../clipboard/native_watch.go#L36-L198), and [`native_watch_test.go`](../../clipboard/native_watch_test.go#L12-L153). Commits [`9b55277`](https://github.com/YMGPwcca/phonelink-linux/commit/9b552776b996f91ab9285305cd10f6feb912b7dd) and [`3c79498`](https://github.com/YMGPwcca/phonelink-linux/commit/3c79498277dc84473091b54fad6a875808f67e7c) record the write and watcher changes. The baseline reports immediate single publications for consecutive copies, one genuine-clear publication, non-echoing phone writes, and clean Ctrl+C without polling fallback.

</details>

## Runtime and service findings

### Shared host and scoped feature traffic

The modular runtime separates a business-agnostic kernel, shared Phone Link host, bounded feature endpoints, and clipboard-owned state and workers. Successful start registers live capabilities and marks the entry Ready; stopping or failing revokes them. The `clipboard-sync` compatibility command uses the same module lifecycle with an in-memory record. Requested feature permissions remain metadata, not a sandbox.

<details>
<summary>Runtime owners and modularization commits</summary>

Owners are [`runtime/`](../../runtime/), [`runtime/phonehost/`](../../runtime/phonehost/), [`features/catalog.go`](../../features/catalog.go), and [`features/clipboard/`](../../features/clipboard/). Commits [`af095a7`](https://github.com/YMGPwcca/phonelink-linux/commit/af095a76d05a609c307f764ebf38f8c76ca7d074), [`1a04f7c`](https://github.com/YMGPwcca/phonelink-linux/commit/1a04f7c5c9943cbb6832bcd16b5fd7bc8e37ffef), [`bc5576f`](https://github.com/YMGPwcca/phonelink-linux/commit/bc5576f549d69eaaf1c129a974cecf6f0929e46d), and [`e75d529`](https://github.com/YMGPwcca/phonelink-linux/commit/e75d5299f5b339cd04014849195d424a961b9fa8) establish the implementation sequence. The [module architecture](../architecture/modules.md) records lifecycle, dependency, endpoint, and permission boundaries.

</details>

### Capability and lifecycle snapshots are not atomic together

Source inspection shows that [`startEntry`](../../runtime/kernel/registry.go#L375-L408) registers capabilities, checks the epoch again, and then marks the entry Ready. [`CapabilityRegistry`](../../runtime/kernel/capability.go#L25-L83) has its own lock. **Inference:** an independent capability reader can observe registration before Ready, including a short-lived registration that a stale-start check later removes. This is a source-level ordering gap in the earlier Ready-only description, not a recorded live failure. The [module reference](../architecture/modules.md) states the synchronization boundary.

### Live CRUD and startup socket ownership

The control socket is versioned and keyed by the absolute feature-store path. Reserving it before host bootstrap prevents feature commands from falling back to offline writes while a daemon starts. Live enable/disable maps to Start/Stop; configuration updates stop and restart enabled instances; delete revokes a module before persistence removal. Persistence failure triggers rollback. Restarts advance the epoch of an existing entry, while delete followed by create begins a new sequence at 1. Service false flags skip actions; they do not undo existing state.

<details>
<summary>Live transaction sources, hardening commits, and CRUD report</summary>

The transaction is implemented in [`runtime_control.go`](../../cmd/linkmyphone/runtime_control.go), with supporting contracts in [`runtime/kernel/`](../../runtime/kernel/) and [`runtime/controlplane/`](../../runtime/controlplane/). Commits [`f1ff937`](https://github.com/YMGPwcca/phonelink-linux/commit/f1ff9372ffda2cf74746424bdcf8071cf8a3566c), [`1a41a38`](https://github.com/YMGPwcca/phonelink-linux/commit/1a41a38fe58d76aefe8dc9a3624b19080539e9f1), and [`3d64a87`](https://github.com/YMGPwcca/phonelink-linux/commit/3d64a87280d747968d8c9655144917b5a636034c) record lifecycle and socket hardening.

The baseline reports live list/get, disable, enable, configuration restart, delete, and create against one running process connected to the same S23, using hashed XDG socket. Final Ctrl+C stopped the recreated module cleanly. That sequence does not establish recovery after losing the cloud session.

</details>

### systemd readiness and graphical-session lifecycle

The user service uses `Type=notify` and follows `graphical-session.target`. Readiness waits for host startup, enabled modules, and live feature controller. Default installation imports graphical environment, installs executable and unit, enables it, and restarts onto the new binary. An empty Wayland selection is accepted as empty text rather than a startup failure.

<details>
<summary>User-service sources and graphical-session validation report</summary>

The implementation is in [`packaging/systemd/`](../../packaging/systemd/), [`service.go`](../../cmd/linkmyphone/service.go), and [`runtime/systemdnotify/`](../../runtime/systemdnotify/). Commit [`5ba71ec`](https://github.com/YMGPwcca/phonelink-linux/commit/5ba71ec96c7bafb2f7a1827693da7005e757c221) records the work.

CachyOS/Wayland reports cover installation, empty-selection startup, restart, stop/start, logout/login, watcher ownership inside the service cgroup, and live feature control. Calls during `activating` received the startup retry error; calls after readiness returned live state. Stop removed this client's watcher while leaving unrelated clipboard-manager watchers alone. No original journal is tracked. Other session managers and init systems remain unvalidated.

</details>

## Remaining gaps

Automatic session recovery, peer re-presence, suspend-gap detection and token renewal are implemented; see [source observations and implementation](../architecture/session-resilience.md). The owner reports successful recovery after Linux network loss, phone network loss and suspend/resume at `a1f53ff`, with bidirectional clipboard working afterwards and desired feature state retained. Scheduled renewal through actual token expiry and multi-hour reliability still need distinct live evidence. Live reports cover one linked S23 and a Wayland desktop; X11, other compositors, other phone models, and Windows WAM execution were not live validated in the tracked record.

Protocol versions, tag 9, enum values, capability versions 14 and 3, and advertised compatibility metadata describe particular contracts or observations. They do not guarantee interoperability with arbitrary peers. The [validation record](validation.md) preserves reported checks and limits; [history](history.md) links corrective commits.

## Rich content and image direction (2026-10-03)

The initial image implementation normalized received JPEG/BMP to PNG and then applied the outbound 1 MiB budget to that desktop PNG. This caused a second resize when the PNG expanded, even though Android had already prepared the transfer. Windows receive creates its clipboard bitmap without that extra resize. Commit `7f999f3` separates incoming normalization from outbound preparation and retains full native image bytes for echo suppression.

The owner subsequently pasted the same phone image on Linux and Windows and reported **1572×2096** on both. For another same-source image sent from the PCs to Android, Linux produced **583×1036**, Windows **310×551**; the owner judged Linux better in that case. Windows uses high-quality bicubic while Linux retains independent area-average resampling and different size selection. These are sample observations, not a universal quality ranking. See [content evidence](clipboard-content.md) and [validation](validation.md#clipboard-validation-2026-10-03).
