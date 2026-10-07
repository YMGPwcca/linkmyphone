# Feature modules

[Documentation index](../docs/README.md)

The catalog registers two in-process modules: clipboard synchronization and Android notification forwarding. Other Phone Link capabilities are not implemented.

| Feature ID | Package | Behavior |
| --- | --- | --- |
| `linkmyphone.clipboard` | [`features/clipboard`](clipboard) | Bidirectional text, HTML and image clipboard sync. |
| `linkmyphone.notifications` | [`features/notifications`](notifications) | Phone notification display, dismissal, app actions and replies when supported. |

## Catalog and module boundaries

[`features/catalog.go`](catalog.go) builds a `features.Definition` for each module: its manifest, default configuration, validator and factory. The catalog resolves only implementations compiled into the program. Add or remove a catalog entry with its feature package; do not add feature-specific branches to the kernel or phone host.

Each feature owns its protocol and native resources. Modules subscribe to the shared `phonehost.Session` through matcher-scoped endpoints; they do not read the raw relay stream. Manifests declare potential capabilities and requested permissions, but permissions are metadata—not authorization or sandboxing. The runtime does not isolate in-process modules.

## Clipboard module

`features/clipboard` owns native clipboard observation and writes, protocol handling, publication queues, generation ordering and echo suppression. Wayland and X11 rich providers poll MIME offers at `poll_interval_ms`; the legacy Wayland text path uses `wl-paste --watch` when available. `xsel` supports text only. Native commands run without a shell.

Clipboard snapshots are eligible for correlated CONTENT responses for two minutes, up to 64 entries and 16 MiB total. Expired snapshots are pruned on the next local publication or CONTENT request, so an idle client can retain expired bytes longer. Retired correlations are trimmed to 256 during pruning and have no time expiry; new retirements can raise the count until the next prune. The three-second origin barrier suppresses reflected content; it is not a secret filter. The `clipboard-sync` compatibility command starts this same module through the modular lifecycle.

## Notification module

`features/notifications` requires a Phone Link (PL) enrollment, granted phone notification access, a session D-Bus notification service, and a successful APP connect/reconcile. It keeps bounded in-memory notification state and exposes only capabilities that are live on the selected peer.

Desktop initialization uses `request_timeout_ms` and honors an earlier caller deadline or cancellation. Once initialization succeeds, the native backend has an explicit lifetime so teardown can close local notifications before closing D-Bus. The native backend invalidates desktop IDs when the notification service owner changes and silently restores visible items after reconnecting. The optional GTK4 reply helper supports up to four cancellable reply windows and requires confirmation before sending. Updates, removals, service resets and shutdown cancel stale prompts; queued actions are rechecked before sending and are not replayed into a replacement session.

Existing phone notifications stay quiet by default. Set `remote_actions=false` for receive-only use. Stopping the module closes local notifications but does not dismiss them on the phone. Clipboard and notifications can share one PL session; existing CrossDevice (WEA) state is not relabeled. See [first-run setup](../docs/getting-started/first-run.md), [notification configuration](../docs/reference/configuration.md#notification-configuration), and [validation limits](../docs/research/validation.md#unified-pl-enrollment-2026-10-05).

## Lifecycle and extension

Modules validate configuration before opening long-lived resources, return only live capabilities, own and stop every worker or endpoint they create, and make `Stop` idempotent. Clipboard `FEATURE_ON`/`FEATURE_OFF` exchanges are advisory; notification startup requires a successful APP connect/reconcile.

Use the clipboard module as the implementation example. A new builtin needs a manifest, configuration validator, module, narrow endpoint matcher and tests before it is registered. To remove one, stop it and its dependents, remove its catalog entry and package, and leave stale records available for inspection or deletion. See the [module architecture](../docs/architecture/modules.md#adding-a-builtin) for the lifecycle contract.

For formats and transfer limits, see [clipboard behavior](../docs/user-guide/clipboard.md). The [clipboard protocol reference](../docs/protocol/clipboard.md) documents wire messages and correlation rules.
