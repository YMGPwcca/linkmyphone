# Configuration and persisted state

[Documentation index](../README.md)

Authentication and feature configuration use separate stores:

| Path                                      | Purpose                       |
| ----------------------------------------- | ----------------------------- |
| `~/.config/linkmyphone/state.json`    | Authentication and enrollment |
| `~/.config/linkmyphone/features.json` | Desired feature records       |

Both defaults use the platform user configuration directory; `XDG_CONFIG_HOME` can change the base directory. `--state` on `bootstrap-probe`, `peer-probe`, `session-probe`, and `run` selects authentication state. On `feature` commands, it selects the feature registry.

Use explicit paths when selecting stores for an account or test profile:

```bash
linkmyphone run \
  --state "$HOME/.config/linkmyphone/state.json" \
  --features-state "$HOME/.config/linkmyphone/features.json"
```

Keep each separately enrolled identity in its own directory. Saving authentication state changes its parent directory to `0700`, even if the directory already exists; do not use a shared directory. Fresh installations enroll PL at the default path. Existing WEA state remains WEA and must not be relabeled. One PL state and feature store can serve both clipboard and notifications.

## Authentication state

`state.json` has schema version `1` and is written atomically. The implementation is [`auth/state/store.go`](../../auth/state/store.go).

| Field | Stored value and purpose |
| --- | --- |
| `version` | State schema version. The current value is `1`; unsupported versions are rejected. |
| `clientProfile` | Persisted `crossdevice` or `phonelink` enrollment identity. Missing legacy values stay CrossDevice; unknown profiles are refused. Do not edit this field to reclassify an existing identity. |
| `logicalDeviceId` | Stable logical device identifier used in CrossDevice headers. |
| `msaRefreshToken` | Microsoft identity refresh credential used to resume sign-in. |
| `identity` | Linux DCG identity key pair: `id`, base64 PKCS#8 private key, and base64 DER certificate. |
| `trustIdentity` | Local trust identity key pair with the same three fields, used for peer trust and signed wake operations. |
| `servicesToken` | The DCG service token record described below. |
| `enrollment` | Account certificate, account metadata, and root certificate chain returned during enrollment. |
| `trustRelationships` | Linked-device trust relationships and peer certificate metadata. |

Both key-pair objects use these exact fields:

| Field | Meaning |
| --- | --- |
| `id` | DCG identity ID, or `trust_<dcgClientId>` for the trust identity. |
| `privateKeyPkcs8` | Base64-encoded PKCS#8 private key. |
| `certificateDer` | Base64-encoded DER certificate. |

<details>
<summary>Enrollment, trust, and service-token fields</summary>

The `enrollment` object contains:

| Field | Meaning |
| --- | --- |
| `accountCert` | Service-returned account certificate. |
| `accountInfo` | Account metadata with optional `accountKey`, `firstName`, `lastName`, and `signInName` fields. Redact these account identifiers. |
| `rootCertificateChain` | Array of service-returned certificate strings. |

Each `trustRelationships` entry has these fields:

| Field | Meaning |
| --- | --- |
| `selfClientId` | Local trust identity ID. |
| `partnerClientId` | Account or peer trust identity ID. |
| `partnerDcgClientId` | Corresponding account or peer DCG ID. |
| `partnerCertificate` | Base64-encoded peer/account certificate. |
| `partnerKeyExpirationTime` | Recorded key-expiry timestamp. |
| `relationshipLastAccessTime` | Recorded last-access timestamp. |
| `msaCid` | Optional Microsoft account CID. |
| `trustType` | Numeric relationship type: constructed account trust uses A2D (`4`), and peer trust uses AsyncD2D (`3`). |
| `attributes` | String map; constructed production relationships include `dcg_prod_client_id`. |

`servicesToken` contains:

| Field                   | Meaning                                           |
| ----------------------- | ------------------------------------------------- |
| `token`                 | DCG `general` services access token.              |
| `scope`                 | Token scope.                                      |
| `deviceId`              | DCG device ID associated with the token.          |
| `expiresAt`             | Token expiry timestamp.                           |
| `keyValidRemainingDays` | Remaining validity reported for the identity key. |
| `tenantId`              | Optional tenant identifier.                       |

</details>

The state loader checks JSON syntax and schema version. Key-pair decoding also checks the certificate ID, key type, and certificate/private-key match. Do not edit these values manually to repair a failed sign-in.

The file contains private key material and refresh credentials. The parent directory is mode `0700`; the state file and temporary replacement file are mode `0600`. These filesystem modes protect against other ordinary local users, but they are not an encryption guarantee and do not protect a compromised account, root, or a process already running as your user.

A normal restart refreshes the Microsoft token and signs the existing DCG identity in. It does not call identity creation again. Do not copy a state file between users or machines unless you understand that it carries the identity and credentials with it.

## Feature registry

`features.json` has schema version `1` and this shape:

```json
{
  "schema_version": 1,
  "features": [
    {
      "id": "linkmyphone.clipboard",
      "enabled": true,
      "config": {
        "poll_interval_ms": 500,
        "request_timeout_ms": 10000,
        "publish_initial": false
      }
    }
  ]
}
```

Each feature record has exactly these fields:

| Field | Meaning |
| --- | --- |
| `id` | Feature identifier. The catalog contains `linkmyphone.clipboard` and `linkmyphone.notifications`. |
| `enabled` | Desired startup state. `run` starts available records with `true`. |
| `config` | Feature-specific JSON object. The built-in clipboard validator rejects unknown properties. |

The registry records desired state, not whether a module is running. Disabled records remain installed. Live snapshots add `state`, `epoch`, and `last_error`; these are not persisted in `features.json`. Live capabilities belong to the kernel capability registry, not the control-plane snapshot.

The store writes mode `0600` files under a mode `0700` directory and replaces them atomically. A missing file is treated as an empty registry. Unknown top-level fields, an unsupported schema version, duplicate IDs, invalid IDs, non-object config, trailing JSON, and malformed JSON are rejected.

## Clipboard configuration

The clipboard feature has ID `linkmyphone.clipboard`, version `0.2.0`, and this default configuration:

| Property | Type and range | Default | Effect |
| --- | --- | --- | --- |
| `poll_interval_ms` | Integer, 50 through 60000 | `500` | MIME observation interval for rich backends; polling fallback for text-only providers. |
| `request_timeout_ms` | Integer, 100 through 120000 | `10000` | Clipboard protocol request timeout. |
| `publish_initial` | Boolean | `false` | Publish the current local clipboard on each module start, including recovery. |

The feature schema is [`features/clipboard/config.schema.json`](../../features/clipboard/config.schema.json). Empty or omitted config is decoded with defaults. `feature create --config JSON` validates the object before writing it. `feature update --config JSON` validates and replaces the entire object. It does not merge keys with the previous object.

To enable initial publication with the default polling interval and request timeout:

```bash
linkmyphone feature update \
  --config '{"poll_interval_ms":500,"request_timeout_ms":10000,"publish_initial":true}' \
  linkmyphone.clipboard
```

The compatibility command's `--poll-interval` and `--publish-initial` flags construct an in-memory record for the same module. They do not write this configuration to `features.json`.

## Notification configuration

`linkmyphone.notifications` requires a genuine `phonelink` enrollment, granted phone notification permission and a desktop session bus service implementing `org.freedesktop.Notifications`. It can share the PL host session with `linkmyphone.clipboard`; legacy WEA state is preserved for clipboard-only operation.

| Property | Type and range | Default | Effect |
| --- | --- | --- | --- |
| `request_timeout_ms` | Integer, 100 through 120000 | `10000` | APP request/desktop-call deadline. |
| `remote_actions` | Boolean | `true` | Permit explicit desktop dismissal, Android action buttons, launch and confirmed reply. Set false for a receive-only trial. |
| `show_existing` | Boolean | `false` | Render existing/reconciled items with the suppress-sound hint; false avoids startup/recovery floods. The hint does not guarantee that a server hides its popup. |

The [notification schema](../../features/notifications/config.schema.json) rejects unknown properties, non-object values, and trailing JSON. The optional reply UI uses Python GI/GTK4. Missing GTK or native action support removes reply capabilities/buttons, not notification reception. Each new host generation reconciles from fresh memory without replaying pending mutations.

State is limited to 4096 items and 32 MiB of estimated retained content. When that budget is exhausted, the oldest item is evicted locally and reported in diagnostics; eviction never dismisses it on the phone. Desktop service loss invalidates native IDs and cancels replies. A new owner silently replays previously visible/pending items. See [`notifications/`](../../notifications/) and [`features/notifications/`](../../features/notifications/).

## Live and offline edits

Feature commands connect to the selected store's control socket. A ready runtime applies changes through its registry and saves them on success. Enabling, disabling, or changing configuration can restart a module without restarting the process. The entry's epoch increases on restart; deleting and recreating it starts a new epoch sequence.

If the socket is absent, refused or invalid, commands read or edit the registry file directly. Use this to configure features before starting the runtime.

During startup or recovery, the socket stays reserved and feature commands ask you to retry: `runtime is starting; retry the feature command` or `runtime is recovering; retry the feature command`. Retry after readiness. Final shutdown rejects new requests and closes the socket. See [session recovery](../operations/session-recovery.md).

The socket is a user-local Unix socket under `$XDG_RUNTIME_DIR/linkmyphone/` with a filename derived from a hash of the absolute feature-store path. If `XDG_RUNTIME_DIR` is unset or the candidate path reaches the 100-byte budget, the runtime uses `/tmp/linkmyphone-<uid>/`. The socket directory is mode `0700` and the socket is mode `0600`.

Registry persistence and atomic replacement are defined in [`runtime/kernel/store.go`](../../runtime/kernel/store.go) and covered by [`runtime/kernel/store_test.go`](../../runtime/kernel/store_test.go). Authentication state is defined in [`auth/state/store.go`](../../auth/state/store.go) and covered by [`auth/state/store_test.go`](../../auth/state/store_test.go). The socket contract is defined in [`runtime/controlplane/protocol.go`](../../runtime/controlplane/protocol.go) and covered by [`runtime/controlplane/controlplane_test.go`](../../runtime/controlplane/controlplane_test.go).
