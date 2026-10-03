# Configuration and persisted state

[Documentation index](../README.md)

Use the two stores for different jobs:

| Path                                      | Purpose                       |
| ----------------------------------------- | ----------------------------- |
| `~/.config/linkmyphone/state.json`    | Authentication and enrollment |
| `~/.config/linkmyphone/features.json` | Desired feature records       |

Both defaults use the platform user configuration directory, so `XDG_CONFIG_HOME` can change the base directory. The stores are independent. `--state` on `bootstrap-probe`, `peer-probe`, `session-probe`, and `run` selects authentication state; `--state` on `feature` commands selects the feature registry.

For a separate account or test profile, choose an explicit path for each store:

```bash
linkmyphone run \
  --state "$HOME/.config/linkmyphone/state.json" \
  --features-state "$HOME/.config/linkmyphone/features.json"
```

Keep each profile in its own directory. Saving authentication state changes its parent directory to `0700`, including an existing directory. Avoid placing a custom state file in a shared directory.

## Authentication state

`state.json` has schema version `1` and is written atomically. The implementation is [`auth/state/store.go`](../../auth/state/store.go).

| Field | Stored value and purpose |
| --- | --- |
| `version` | State schema version. The current value is `1`; unsupported versions are rejected. |
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
| `accountInfo` | Account metadata with optional `accountKey`, `firstName`, `lastName`, and `signInName` fields. These identify the account and belong in redaction. |
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
| `id` | Feature identifier. The current catalog contains `linkmyphone.clipboard`. |
| `enabled` | Desired startup state. `run` starts available records with `true`. |
| `config` | Feature-specific JSON object. The built-in clipboard validator rejects unknown properties. |

The registry is desired state, not proof that a module is currently running. A disabled record remains installed. A live runtime snapshot adds operational fields such as `state`, `epoch`, and `last_error`; these are not persisted in `features.json`. Live capabilities belong to the kernel capability registry and are not fields in the control-plane snapshot.

The store writes mode `0600` files under a mode `0700` directory and replaces them atomically. A missing file is treated as an empty registry. Unknown top-level fields, an unsupported schema version, duplicate IDs, invalid IDs, non-object config, trailing JSON, and malformed JSON are rejected.

## Clipboard configuration

The catalog's only built-in feature has ID `linkmyphone.clipboard`, version `0.2.0`, and this default configuration:

| Property | Type and range | Default | Effect |
| --- | --- | --- | --- |
| `poll_interval_ms` | Integer, 50 through 60000 | `500` | MIME observation interval for rich backends; polling fallback for text-only providers. |
| `request_timeout_ms` | Integer, 100 through 120000 | `10000` | Clipboard protocol request timeout. |
| `publish_initial` | Boolean | `false` | Publish the current local clipboard on each module start, including recovery. |

The feature schema is [`features/clipboard/config.schema.json`](../../features/clipboard/config.schema.json). Empty or omitted config is decoded with defaults. `feature create --config JSON` validates the object before writing it. `feature update --config JSON` validates and replaces the entire object. It does not merge keys with the previous object.

For example, preserve the default request timeout while enabling initial publication by sending both values explicitly:

```bash
linkmyphone feature update \
  --config '{"poll_interval_ms":500,"request_timeout_ms":10000,"publish_initial":true}' \
  linkmyphone.clipboard
```

The compatibility command's `--poll-interval` and `--publish-initial` flags construct an in-memory record for the same module. They do not write this configuration to `features.json`.

## Live and offline edits

Feature commands first try the control socket derived from the selected feature-store path. A ready runtime handles list, get, create, update, and delete against its in-memory registry and persists successful desired-state changes. Enabling, disabling, or changing config reconciles the module without restarting the process. A config or enabled-state transition can stop and restart the module; the registry epoch increases when the same entry is restarted. Deleting and recreating an entry starts its epoch sequence again.

When the socket is absent, refused, or invalid, commands read or write the selected registry file without starting a runtime. This fallback is useful for preparing state before the first run. It does not authenticate, contact the phone, or confirm that a feature can start.

During runtime startup the socket exists before the live controller is installed. Commands receive `runtime is starting; retry the feature command` rather than silently changing offline state. During recovery the same socket returns `runtime is recovering; retry the feature command`; old mutation contexts are cancelled and handlers drained before module replacement. During final shutdown the control plane rejects requests and the socket closes. See [session recovery](../operations/session-recovery.md).

The socket is a user-local Unix socket under `$XDG_RUNTIME_DIR/linkmyphone/` with a filename derived from a hash of the absolute feature-store path. If `XDG_RUNTIME_DIR` is unset or the candidate path reaches the 100-byte budget, the runtime uses `/tmp/linkmyphone-<uid>/`. The socket directory is mode `0700` and the socket is mode `0600`.

Implementation and test references: [`runtime/kernel/store.go`](../../runtime/kernel/store.go) defines registry persistence and replacement behavior; [`runtime/kernel/store_test.go`](../../runtime/kernel/store_test.go) covers it; [`auth/state/store.go`](../../auth/state/store.go) and [`auth/state/store_test.go`](../../auth/state/store_test.go) cover authentication state; the live socket contract is in [`runtime/controlplane/protocol.go`](../../runtime/controlplane/protocol.go) and [`runtime/controlplane/controlplane_test.go`](../../runtime/controlplane/controlplane_test.go).
