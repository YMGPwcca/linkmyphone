# Contributing

[Documentation index](../README.md)

Review the [current support status](../../README.md#current-support) before changing behavior. This guide covers contribution workflow and code ownership.

LinkMyPhone targets Linux with Go 1.23 or newer. Microsoft cloud services are part of the runtime path. Native clipboard synchronization also needs a supported desktop utility, such as `wl-paste` and `wl-copy` on Wayland, or `xclip` or `xsel` on X11.

The Go module is `github.com/YMGPwcca/linkmyphone`; use that path for project imports and `cmd/linkmyphone` for the CLI.

The project uses the [MIT License](../../LICENSE). GitHub Actions runs on pushes to `main`; run the local checks in [testing](testing.md) before submitting.

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
| A feature's protocol, native resources, workers, matchers, queues, configuration, or diagnostics | `features/clipboard` or `features/notifications` | Direct reads from `transport/relay.Client.Received()` |
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


## Submit a pull request

Start a topic branch from current `main` with no unrelated local changes:

```bash
git switch main
git pull --ff-only
git switch -c feature/describe-the-change
```

Format changed Go files with `gofmt`, run the relevant package tests and full Linux suite, and exercise the affected runtime path when its environment is available. Keep live Microsoft and clipboard checks separate from deterministic tests.

Use a commit subject that names the behavior, following the existing `feat`, `fix`, or `docs` convention. Push the topic branch to a repository where you have write access and open a pull request against `main`. Repository access is separate from Microsoft account sign-in.

Describe the changed behavior, reproduction steps, checks run, and untested environments in the pull request. For protocol changes, link the finding, source or fixture evidence, and regression test. Redact credentials, device identifiers, private paths, and clipboard contents from attached output.

## Built-in modules

Feature modules are compiled into the executable; runtime API 1.0 does not support plugin loading, out-of-process modules or sandboxing. See the [module architecture](../architecture/modules.md#adding-a-builtin) for the lifecycle contract and the [feature guide](../../features/README.md) for catalog ownership and extension steps.
