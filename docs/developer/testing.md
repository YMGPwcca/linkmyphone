# Testing

[Documentation index](../README.md)

Use the smallest test command that owns the behavior, then run the full Linux suite before submitting. Tests are Go package tests. The deterministic suite uses fakes, temporary stores, protocol fixtures, and injected transports. It does not require Microsoft authentication or a desktop clipboard session.

## Supported commands

Run on Linux from the repository root with Go 1.23 or newer. The full suite exercises Unix sockets and file permissions. The race detector also needs a CGO-enabled toolchain and a C compiler.

```bash
go test ./...
go test -race ./...
```

Choose the lane that matches the evidence you need:

| Lane | Prerequisites | Proves |
| --- | --- | --- |
| Package and full suite | Linux, Go 1.23+ | Deterministic behavior using isolated fakes and fixtures. |
| Race detector | Linux, CGO, C compiler | Data races exercised by the suite. |
| Cloud probe | Microsoft account, network, linked device | A finite exchange with live services, not regression coverage. |
| Desktop smoke | Linux graphical session, clipboard tools | Visible clipboard behavior in that session. |

<details>
<summary>Test lane chart</summary>

```mermaid
flowchart TD
    Change[Changed owner] --> Focus[Focused package test]
    Focus --> Full[go test ./...]
    Full --> Race[go test -race ./...<br/>Linux, CGO, C compiler]
    Change --> Cloud[Opt-in cloud probes<br/>Microsoft account, network, linked device]
    Change --> Desktop[Opt-in desktop smokes<br/>Linux session and clipboard tools]
    Cloud --> Evidence[Manual evidence<br/>not deterministic regression coverage]
    Desktop --> Evidence
```

</details>

### Package commands

The same package commands are useful while changing one owner:

```bash
go test ./runtime/kernel
go test ./runtime/phonehost
go test -race ./runtime/phonehost ./bootstrap ./transport/signalr ./transport/wsclient
go test ./runtime/controlplane
go test ./features/...
go test ./clipboard ./protocol/... ./transport/...
go test ./cmd/linkmyphone
```

Use a package's existing test names for a focused run, for example:

```bash
go test ./runtime/kernel -run 'TestRegistry|TestFeatureStore'
go test ./runtime/phonehost -run 'TestRouter|TestSession'
go test ./runtime/controlplane -run 'TestServer|TestListen|TestCall|TestSocket'
go test ./cmd/linkmyphone -run 'TestFeatureCommand'
go test ./features/clipboard -run 'TestConfig|TestManifest|TestInstanceStop'
```

The CI workflow at [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) runs only on pushes to `main` (merged pull requests or direct pushes); feature pushes, open pull requests and manual dispatch do not trigger it. It uses the Go version declared by `go.mod`, runs vet and build, then runs the full race suite under Xvfb with real X11 MIME transfers. Format changed Go files with `gofmt`. No separate linter, release command, or coverage threshold is configured here.

## Invariant map

Run the test package that owns the invariant after changing its implementation. The full suite remains the final local check.

| Consumer-visible invariant | Tests and owner |
| --- | --- |
| Unknown manifest fields, trailing JSON, unsafe schema paths, duplicates, invalid IDs, incompatible API versions, and incomplete metadata are rejected | `runtime/kernel/manifest_test.go`, `runtime/kernel/manifest.go` |
| Feature records survive reopen, duplicate IDs fail, configs are JSON objects, saves are atomic and private | `runtime/kernel/store_test.go`, `runtime/kernel/store.go` |
| Required providers start before dependents; missing providers block; compatible versions are required; provider teardown is protected | `runtime/kernel/registry_test.go`, `runtime/kernel/registry.go` |
| An old instance cannot mutate a replacement epoch; dependency failure degrades active dependents; a stale start is rolled back and its capabilities never become live | `runtime/kernel/registry_test.go` |
| Stop removes capabilities and remains safe to repeat; stopped update preserves the stopped lifecycle fact | `runtime/kernel/registry_test.go`, `features/clipboard/module_test.go` |
| The raw relay stream has one receiver; matching messages fan out with copied payloads; a slow bounded endpoint is revoked without harming another endpoint | `runtime/phonehost/router_test.go`, `runtime/phonehost/session_test.go` |
| Recovery retries transient failures, pins the phone, honors throttling, renews tokens, detects resume/peer loss and preserves rotated credentials | `runtime/phonehost/resilience_test.go`, `bootstrap/resume_persistence_test.go`, `transport/signalr/client_test.go`, `transport/wsclient/shutdown_test.go` |
| Endpoint close revokes sending and closes receiving; router cancellation and no-subscriber draining work | `runtime/phonehost/router_test.go` |
| Control requests use version 1, unknown fields fail, socket paths are store-scoped and short, permissions are `0700` for the directory and `0600` for the socket, and stale sockets are replaced safely | `runtime/controlplane/controlplane_test.go` |
| Startup and shutdown handler transitions are visible without replacing the socket; handler panic is isolated; active handlers drain on close | `runtime/controlplane/controlplane_test.go`, `cmd/linkmyphone/runtime_run.go` |
| Connected CLI mutations do not fall back to offline persistence when the runtime rejects them; stale records can be disabled and deleted | `cmd/linkmyphone/feature_test.go` |
| Offline feature create, update, toggle, delete, manifest lookup, and default configuration follow the catalog contract | `cmd/linkmyphone/feature_test.go`, `features/catalog_test.go` |
| Clipboard config rejects unknown and out-of-range values and the manifest exposes text capabilities plus HTML/image capabilities on rich providers | `features/clipboard/config_test.go` |
| Clipboard stop does not spend the full request timeout on advisory `FEATURE_OFF` and is idempotent after cancellation | `features/clipboard/module_test.go` |
| Clipboard protocol codecs, platform routes, MSAEP envelopes, DCG fragments, SignalR framing, and relay transport preserve their wire contracts | `protocol/**`, `transport/**`, `clipboard/*_test.go` |
| Native clipboard commands avoid a shell, detect supported Wayland or X11 utilities, normalize empty Wayland selection, and frame watch-helper events | `clipboard/native_test.go`, `clipboard/native_watch_test.go` |
| Clipboard generation ordering, correlation snapshots, stale publication rejection, echo suppression, and request handling remain deterministic | `clipboard/client_test.go`, `clipboard/generation_test.go`, `clipboard/generation_order_test.go`, `clipboard/retired_snapshot_test.go` |
| Wayland event coalescing, nil-selection debounce, polling fallback, and shutdown ownership remain bounded | `features/clipboard/sync_test.go`, `features/clipboard/matcher_test.go`, `clipboard/native_watch_test.go` |
| Auth state persistence, identity primitives, device-code handling, trust, and bootstrap stage transitions preserve their local contracts | `auth/**/*_test.go`, `bootstrap/**/*_test.go` |
| Header profiles and DCG service request behavior remain source-compatible | `dcgheaders/**/*_test.go`, `services/dcg/**/*_test.go` |
| systemd readiness notification reports the runtime state without changing module ownership | `runtime/systemdnotify/*_test.go` |

Tests are not substitutes for the ownership rules. When changing a queue, endpoint, or worker, add or update a test that observes the feature or shared host behavior, not just an internal field or a mock call count.

## Microsoft cloud probes

These commands are opt-in production scenarios. They require a Microsoft account, linked devices, network access, and a safe local state path. Do not run them in automated tests or with credentials in command arguments. They may create or update `~/.config/linkmyphone/state.json`, which contains refresh credentials and private key material.

```bash
go run ./cmd/linkmyphone bootstrap-probe
go run ./cmd/linkmyphone peer-probe
go run ./cmd/linkmyphone session-probe
go run ./cmd/linkmyphone session-probe --context-probe
```

The first probe performs device-code login, DCG identity enrollment, trust refresh, and SignalR connection. Later probes reuse persisted identity state. `peer-probe` selects or wakes a linked Android peer. `session-probe` validates PLATFORM `/SessionValidation`; `--context-probe` observes the tag-9 clipboard publication path without returning clipboard content unless an explicit probe text is supplied. These probes exercise Microsoft cloud behavior and should be treated as manual evidence, not deterministic regression tests.

The runtime path uses the same cloud stages:

```bash
go run ./cmd/linkmyphone feature create --enabled linkmyphone.clipboard
go run ./cmd/linkmyphone run
```

With a running `run` process, feature CRUD uses the hashed Unix socket and reports live state and epochs. Local regression tests now cover recovery supervision, peer-loss and suspend detection, token-renewal scheduling, rotated-credential persistence, and transport deadlines. Real cloud recovery and multi-hour operation remain unvalidated; follow [session recovery checks](../operations/session-recovery.md#live-checks). See the [validation research](../research/validation.md) for historical reports and their environment.

## Desktop and systemd smokes

These scenarios need a Linux desktop session and native clipboard utilities. The opt-in X11 provider test is included in CI; authenticated phone scenarios remain manual:

```bash
go run ./cmd/linkmyphone clipboard-sync
go run ./cmd/linkmyphone clipboard-sync --poll-interval 250ms
go run ./cmd/linkmyphone service install
go run ./cmd/linkmyphone service status
go run ./cmd/linkmyphone service logs --follow
go run ./cmd/linkmyphone service restart
go run ./cmd/linkmyphone service uninstall
```

Rich Wayland and X11 providers poll MIME offers. Empty selections are debounced and re-read before publication. The legacy text watcher remains available for text-only providers. A desktop smoke should observe local copy, phone-to-Linux write, reflected-echo suppression, genuine clear, and clean Ctrl+C or service stop. Keep clipboard text non-sensitive. `service install` changes the user executable and systemd unit, so use a disposable user environment when testing installation behavior.

The archived project findings include dated Wayland, S23, and systemd validation reports, plus race-test repetitions. Those reports are evidence of the environments described at the time, not a promise that an untested machine or a current Microsoft service will behave the same way.

## Rich clipboard regression and live evidence

```bash
go test -race ./clipboard ./features/clipboard ./protocol/clipboard
LINKMYPHONE_NATIVE_INTEGRATION=1 xvfb-run -a go test -race ./...
```

The second command requires Xvfb, xclip, Python GI and GTK4, matching CI. Content tests cover explicit-empty text, UTF-16 limits, immutable typed snapshots, fragmented image payloads, malformed/oversized input, BMP/JPEG conversion, incoming dimension preservation, and the outbound budget on both snapshots and live CONTENT fallback. Native tests check MIME preference, bounded command output, full PNG observer/cache bytes above 1 MiB and actual X11 text/HTML/image round-trips. Feature tests cover format-aware echo suppression.

[CI run 37098891014](https://github.com/YMGPwcca/linkmyphone/actions/runs/37098891014) passed vet, build and the full race suite with X11 transfers at `7f999f3`, before CI was restricted to main. Owner-reported live checks on 2026-10-03 now establish received HTML and bidirectional image pastes on Wayland/S23, including image comparisons with Windows. Those reports are recorded separately in [validation](../research/validation.md#clipboard-validation-2026-10-03); automated tests do not replace them. Run local checks before merging because this workflow runs after main updates, not as a pre-merge gate.
