# Modular feature architecture

Phone Link Linux uses a small in-process microkernel so protocol features can be added, disabled, configured, restarted, and removed without moving feature behavior into transport/bootstrap code.

The design borrows the same core principles used by OMO: strict manifests, explicit lifecycle, potential-vs-live capabilities, bounded queues, revocable ownership, dependency-aware teardown, and instance epochs.

## Boundaries

```text
cmd/phonelink-linux
       |
       v
features/catalog.go          <- build composition only
       |
       +--------------------------+
       |                          |
       v                          v
runtime/kernel            runtime/phonehost
  manifests                 auth / trust
  desired-state CRUD        wake / SignalR
  dependency graph          SessionValidation
  lifecycle + epochs        feature message router
  live capabilities         scoped endpoints
       |                          |
       +------------+-------------+
                    |
                    v
             features/<id>
                    |
                    v
          protocol/domain clients
```

### `runtime/kernel`

The kernel is business-agnostic. It must not know clipboard, notifications, calls, photos, or any Phone Link feature route.

It owns:

- strict manifest parsing and runtime API compatibility;
- persistent desired-state records;
- module dependency resolution;
- lifecycle transitions and instance epochs;
- live capability publication/revocation;
- safe stop/delete rules for required dependencies;
- generic structured reporting.

### `runtime/phonehost`

The phone host owns shared connection infrastructure:

- persisted Microsoft/DCG identity resume;
- trust refresh and Android peer selection;
- Hub Relay connection and signed wake;
- PLATFORM `/SessionValidation`;
- exactly one consumer of `relay.Received()`;
- fan-out to bounded feature-scoped subscriptions.

Feature modules never consume the raw relay receive channel directly. Each module owns a matcher and receives only its scoped traffic through a revocable endpoint. This prevents future modules from stealing messages from each other.

### `features/<feature>`

A feature owns all business behavior for its domain: protocol routing, native integration, queues, goroutines, configuration, and feature-specific diagnostics.

A feature may depend on kernel/host contracts and lower-level protocol packages. Feature packages must not import each other directly. Cross-feature coordination belongs behind an explicit contract/capability.

## Manifest contract

Every built-in feature has a strict JSON manifest. Unknown fields fail closed.

The canonical schema is:

`contracts/manifests/phonelink-feature.schema.json`

A manifest declares:

- stable module ID and semantic version;
- runtime API version;
- potential capabilities;
- required and optional module dependencies;
- requested permissions;
- feature configuration schema;
- diagnostic metadata.

Potential capabilities are not live capabilities. They become discoverable only after the instance reaches `Ready`, and are revoked before/during stop or failure.

Requested permissions are declarative metadata in runtime API v1. They document ownership/security intent but are not yet a sandbox or authorization grant. Enforcement belongs in a future host policy layer; modules must not treat declaration as proof of access.

Version 1 supports `kind: "builtin"` only. Out-of-process modules require a separate versioned control bridge; they must not be smuggled into the built-in ABI.

## Lifecycle

The kernel lifecycle is:

```text
Validated -> Resolved -> Starting -> Ready
               |             |        |
               v             v        v
            Blocked        Failed   Degraded
                                      |
                                      v
                                  Stopping -> Stopped
```

Important rules:

1. Start/stop/update/delete for one module are serialized.
2. Every successful start advances an instance epoch.
3. Late failures from an older epoch cannot mutate a replacement instance.
4. Required dependencies must be `Ready` before a dependent starts.
5. A required provider cannot be stopped/deleted while an active dependent owns it.
6. `StopAll` tears down the reverse of the actual start order.
7. Module stop is idempotent.
8. Queues are bounded; overflow becomes an explicit runtime error rather than hidden transport starvation.

## Desired-state CRUD

The persistent store defaults to:

```text
~/.config/phonelink-linux/features.json
```

The CLI manages built-in feature desired state:

```bash
phonelink-linux feature list
phonelink-linux feature get phonelink.clipboard
phonelink-linux feature create --enabled phonelink.clipboard
phonelink-linux feature update --config '{"poll_interval_ms":250,"request_timeout_ms":10000}' phonelink.clipboard
phonelink-linux feature disable phonelink.clipboard
phonelink-linux feature enable phonelink.clipboard
phonelink-linux feature delete phonelink.clipboard
```

When no daemon is running, these commands mutate persistent desired state directly. When `phonelink-linux run` owns the matching store, it also owns a versioned Unix socket at `runtime.sock` in the same directory. The CLI routes CRUD through that socket and the runtime applies persistence and lifecycle reconciliation in one serialized control path.

The socket directory is forced to `0700` and the socket to `0600`. A stale socket is removed only when a local connection proves there is no live runtime. The socket is reserved before Phone Link bootstrap begins and initially responds with `runtime is starting`; this prevents CLI commands from falling back to offline writes while a daemon is still booting. During shutdown, the handler switches to `runtime is stopping`, in-flight mutations inherit runtime cancellation, and the server drains them before module teardown.

Live update semantics are:

- create: validate/build/register, optionally Start, then persist; persistence failure rolls the runtime entry back;
- update config: Stop an active instance, Update config, Start again when enabled, then persist; failures restore the previous runtime record;
- enable/disable: Update desired enabled state and Start/Stop immediately;
- delete: Delete/Stop the runtime entry first, then remove persistence; persistence failure restores the runtime entry;
- list/get: return both persisted records and live kernel snapshots, including state and epoch.

A removed implementation does not make its persisted record undeletable: stale records remain readable, disable-able, and delete-able. They cannot be enabled or reconfigured until an implementation with that ID is present again.

## Safe feature removal

Removing a feature implementation must not require editing kernel or phone-host business logic.

For a built-in feature:

1. disable/stop the module;
2. revoke its live capabilities and feature endpoint;
3. ensure dependents are stopped first;
4. remove its definition from `features/catalog.go`;
5. remove `features/<feature>`;
6. stale desired-state records may then be deleted through generic CRUD.

The composition catalog is allowed to change because it is build composition metadata, not kernel behavior.

## Concurrency and ownership

The owner of mutable state is explicit:

- DCG send ordering: `transport/relay.Client`, serialized per peer;
- raw receive stream: `runtime/phonehost.Router`;
- feature receive queue: feature-scoped endpoint;
- clipboard request dispatch: `clipboard.Client`;
- clipboard cross-device ordering: one generation domain in `clipboard.Client`;
- native clipboard echo suppression: `features/clipboard`.

Clipboard local and phone changes share a generation sequence once observed by the runtime. A phone publication tombstones older outbound snapshots; stale CONTENT requests are rejected rather than answered with unrelated current text. A newer observed local event prevents an older phone response from overwriting it. The current Wayland watcher still polls, so a compositor-local change that occurs but has not yet been observed cannot participate in ordering until the next poll.

## Adding a feature

Create:

```text
features/<feature>/
  manifest.json
  config.schema.json
  config.go
  module.go
  ...
```

Then add one factory entry to `features/catalog.go`.

The module should:

- keep protocol/native business code inside its feature boundary;
- subscribe through `phonehost.Session.Subscribe`;
- use a narrow matcher;
- return explicit live capabilities;
- own and stop every goroutine/resource it creates;
- use bounded queues with documented overflow behavior;
- validate configuration before start;
- make stop idempotent;
- add race/concurrency tests for its ownership rules.

Do not add feature-specific switches to `runtime/kernel` or `runtime/phonehost`.
