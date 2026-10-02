# Contributing

[Documentation index](../README.md)

This page is the short path from an observed behavior to an owner, a focused change, and a reviewable pull request.

The project goal is an independent, unofficial Phone Link client for Linux with broader phone–PC integration. Clipboard synchronization is the first implemented feature, not a restriction on contributions. Additional features should use the shared host and module contracts described below. Keep proposed functionality separate from implemented and validated support; see [current support and broader scope](../../README.md#current-support-and-broader-scope).

LinkMyPhone targets Linux with Go 1.23 or newer. Microsoft cloud services are part of the runtime path. Native clipboard synchronization also needs a supported desktop utility, such as `wl-paste` and `wl-copy` on Wayland, or `xclip` or `xsel` on X11.

The Go module is `github.com/YMGPwcca/linkmyphone`; use that path for project imports and `cmd/linkmyphone` for the CLI.

The repository has no tracked license file or configured release process. Do not assume redistribution terms. The automated workflow runs `go test ./...` only when manually dispatched; use the local checks in [testing](testing.md) before submitting a change.

## Find the owner before editing

1. Start with the [source map](source-map.md) and identify the narrowest owner.
2. Read that subsystem's contract page, nearest implementation, and tests.
3. Confirm whether the behavior is persisted desired state, a live snapshot, or a cloud and desktop side effect.
4. Keep the change at the owning layer. Do not add a feature-specific switch to the kernel or phone host.

### Ownership index

| If you are changing... | Start here | Keep out of this owner |
| --- | --- | --- |
| Generic manifests, desired-state records, lifecycle, dependency resolution, epochs, capabilities, or reports | `runtime/kernel` | Clipboard or Phone Link feature branches |
| Microsoft and DCG resume, trust, wake, SessionValidation, the raw relay receive loop, or feature endpoints | `runtime/phonehost` | Feature-specific protocol or native behavior |
| Local JSON-over-Unix-socket requests and responses | `runtime/controlplane` | Microsoft or Hub Relay protocol |
| Persistence plus live registry transactions | `cmd/linkmyphone/runtime_control.go` | Feature-domain behavior |
| Builtin composition | `features/catalog.go` | Feature implementation details |
| A feature's protocol, native resources, workers, matchers, queues, configuration, or diagnostics | The feature package, currently `features/clipboard` | Direct reads from `transport/relay.Client.Received()` |
| Wire codecs | `protocol/` | Lifecycle or feature ownership |
| WebSocket, SignalR, or relay transport | `transport/` | Feature routing decisions |
| Identity, trust, state, or cloud startup | `auth/` and `bootstrap/` | Clipboard behavior |

A feature subscribes through `phonehost.Session.Subscribe`; it never consumes `transport/relay.Client.Received()` directly. The phone host remains the sole raw receiver so features cannot steal each other's messages.

## Change workflow

1. Reproduce or locate the behavior at the owning boundary.
2. Reuse the existing contract. Update all callers when the contract changes.
3. Add or update a consumer-visible invariant test. Keep persistence records and live snapshots distinct in code and documentation.
4. Keep Microsoft authentication and desktop clipboard checks opt-in. Never put credentials or private state in fixtures, logs, arguments, or documentation.
5. Update the relevant documentation page when a contract, command, state path, or limitation changes.
6. Run the focused checks, then the full Linux suite described in [testing](testing.md).

For a builtin, follow the complete `features/clipboard` implementation. Add a strict manifest, configuration schema and Go validator, lifecycle implementation, narrow matcher, bounded queues, cleanup, and tests before adding the catalog definition.

### Keep these boundaries explicit

These details are easy to blur during a refactor:

- `run --request-timeout` controls the host's PLATFORM `/SessionValidation` wait. Clipboard operations use the separate module `request_timeout_ms` configuration.
- Service helper false flags skip actions. They do not disable an already enabled unit or stop a process that is already running.
- Authentication `Save` chmods the existing parent directory. The CLI first-run path saves before trust, while the library first-run path saves after trust.
- Capability registration occurs before lifecycle `Ready`; the capability and module registries use separate locks, so their snapshots are not atomic together.
- ACK success can complete a send before Hub `Completion`. Negative ACK, rejection, and cancellation fail without retry.
- Unknown clipboard correlation fallback excludes retired and superseded IDs. Duplicates do not retire active snapshots. Snapshots last two minutes and are capped at 64; retired IDs are capped at 256 with no time expiry.
- The clipboard echo barrier lasts three seconds and tracks text hashes. It is not sensitive-clipboard filtering.

These are contract facts, not implementation trivia. Keep them in tests and documentation when changing the relevant owner.

## Submit a pull request

Start a topic branch from current `main` with no unrelated local changes:

```bash
git switch main
git pull --ff-only
git switch -c feature/describe-the-change
```

Format the Go files you changed with `gofmt`, run the relevant package tests and full Linux suite, and exercise the affected runtime path when its environment is available. Keep live Microsoft and clipboard checks separate from deterministic tests.

Commit the change with a message that names the behavior, following the existing `feat`, `fix`, or `docs` subjects. Push your topic branch to a repository where you have write access and open a pull request against `main`. Repository access must be granted separately from Microsoft account sign-in.

In the pull request, describe the changed behavior, reproduction steps, checks actually run, and environments you did not test. For a protocol change, link the finding, source or fixture evidence, and corrective test. Redact credentials, device identifiers, private paths, and clipboard contents before attaching output.

## Adding a builtin

A builtin is an in-process Go package. There is no supported plugin loader, out-of-process bridge, sandbox, or permission enforcement in runtime API 1.0.

Use this sequence:

1. Create `features/<id>/manifest.json`, `config.schema.json`, config decoding and validation, module lifecycle, and domain implementation.
2. Make `Module.Start` validate configuration before long-lived allocations and return an instance that exposes only genuinely live capabilities, reports asynchronous failure, and stops idempotently.
3. Subscribe through the shared `phonehost.Session` with the narrowest matcher and a bounded queue. Own and close every endpoint, goroutine, timer, child process, and native resource.
4. Add one entry in `features/catalog.go` with manifest, defaults, validator, and factory.
5. Add tests for manifest and config rejection, capability publication and revocation, queue overflow or coalescing, lifecycle ownership, stale work, and shutdown.
6. Verify deterministic tests first. Use live Microsoft and desktop scenarios only when the environment and credentials are explicitly available.

A feature's `permissions.requested` list documents intent. It does not authorize access, and it cannot sandbox code in the process. A removed implementation is removed from the catalog and package. Its persisted record stays generic data so users can disable or delete it.

## Removing a builtin

Disable the feature, stop dependents before required providers, confirm that the endpoint and live capabilities are revoked, then remove its catalog entry and package. Leave no kernel or phone-host special case for the deleted feature. The generic CLI must still list, disable, and delete stale records. It must reject enabling or reconfiguring those records until an implementation with the same ID is restored.

## Useful commands

The following commands are the repository's documented entry points:

```bash
go run ./cmd/linkmyphone help
go run ./cmd/linkmyphone feature list
go run ./cmd/linkmyphone run
go run ./cmd/linkmyphone bootstrap-probe
go run ./cmd/linkmyphone peer-probe
go run ./cmd/linkmyphone session-probe
go run ./cmd/linkmyphone clipboard-sync
go run ./cmd/linkmyphone service install
```

Probe commands use Microsoft services and can update local identity state. The regular deterministic test suite does not require Microsoft authentication. See [testing](testing.md) for exact package commands and the separation between local tests and opt-in smoke scenarios.
