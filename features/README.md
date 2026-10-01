# Feature modules

Each directory under `features/` is a bounded business module. The runtime kernel does not import feature packages; `features/catalog.go` is the build-composition layer.

Current modules:

- `phonelink.clipboard` — bidirectional text clipboard synchronization.

A module directory should contain a strict `manifest.json`, a configuration schema, configuration decoding/validation, lifecycle implementation, protocol/native ownership, and tests.

See `docs/architecture/MODULE_SYSTEM.md` before adding a module.

Do not consume `transport/relay.Client.Received()` directly from a feature. Subscribe through `runtime/phonehost.Session` so the host remains the sole raw-receive owner.
