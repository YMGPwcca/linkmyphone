# Development history

[Documentation index](../README.md)

The baseline chronology below follows the commit ancestry ending at main merge commit [`e51728b`](https://github.com/YMGPwcca/phonelink-linux/commit/e51728b12a148061c8076c67200ee4a8f366e423), the merge of PR #1. Every date below is a commit date in the main ancestry. The implementation was developed on 2026-09-30 and 2026-10-01.

A commit title identifies an implementation milestone. It does not prove that a live service or phone accepted the behavior. Historical live results are in [`validation.md`](validation.md) and [`findings.md`](findings.md).

## Repository and protocol foundation

| Date | Commit | Title |
| --- | --- | --- |
| 2026-09-30 | [`e6a55d8`](https://github.com/YMGPwcca/phonelink-linux/commit/e6a55d826b19487578b0591123182e660328d439) | `chore: initialize repository` |
| 2026-09-30 | [`85c052d`](https://github.com/YMGPwcca/phonelink-linux/commit/85c052d55b4a3bffbf44909d1ed768695babcd19) | `chore: initialize Go module` |
| 2026-09-30 | [`aac8b59`](https://github.com/YMGPwcca/phonelink-linux/commit/aac8b59ab82431e7a99dbf238db17645fbcd2b5a) | `feat(clipboard): implement clipboard protocol codec` |
| 2026-09-30 | [`223f4fa`](https://github.com/YMGPwcca/phonelink-linux/commit/223f4fa036a21d2d7be072dc5a5fdbcb3e1364de) | `feat(platform): implement binary message framing` |
| 2026-09-30 | [`73f1d6b`](https://github.com/YMGPwcca/phonelink-linux/commit/73f1d6b62f69c80fbb509c89e63702dff3b36093) | `feat(dcg): implement transport fragmentation` |
| 2026-09-30 | [`e2c128d`](https://github.com/YMGPwcca/phonelink-linux/commit/e2c128db98062b103cd02d89dd891ac5b70997a9) | `feat(signalr): implement MessagePack frame lengths` |
| 2026-09-30 | [`43c7081`](https://github.com/YMGPwcca/phonelink-linux/commit/43c70810a29658004300a6b9e1d10d51b717bc10) | `feat(signalr): implement Hub Relay invocations` |
| 2026-09-30 | [`11aa31e`](https://github.com/YMGPwcca/phonelink-linux/commit/11aa31e7659c7b033c6a2172a0607225fd525b23) | `feat(transport): add WebSocket and SignalR clients` |
| 2026-09-30 | [`b6c1d7d`](https://github.com/YMGPwcca/phonelink-linux/commit/b6c1d7d719ae7a0b55478b2486119d76cff3c369) | `feat(signalr): decode Hub protocol messages` |
| 2026-09-30 | [`0eeba17`](https://github.com/YMGPwcca/phonelink-linux/commit/0eeba17c89acbdc836a1fa08eb02f28a39d2e728) | `feat(dcg): implement packet reassembly and acknowledgements` |
| 2026-09-30 | [`3e457ad`](https://github.com/YMGPwcca/phonelink-linux/commit/3e457ad60e45f1129f2d6b5efa6875140dd6d9be) | `feat(signalr): parse Hub message types` |
| 2026-09-30 | [`044534c`](https://github.com/YMGPwcca/phonelink-linux/commit/044534c6ff0dcd625c6acfa524b446a649e23dd3) | `feat(relay): implement ACK-aware DCG transport` |
| 2026-09-30 | [`6c82753`](https://github.com/YMGPwcca/phonelink-linux/commit/6c82753ae99f0176c98d310f58f24f0465891239) | `feat(platform): implement DeviceResourceManager responses` |
| 2026-09-30 | [`49efed5`](https://github.com/YMGPwcca/phonelink-linux/commit/49efed5) | `feat(clipboard): add DeviceResourceManager client` |
| 2026-09-30 | [`a4ffac0`](https://github.com/YMGPwcca/phonelink-linux/commit/a4ffac0bc6a9aa6b32adfa1473a4e4d72cada257) | `feat(clipboard): model directional PubSub payloads` |
| 2026-09-30 | [`63a72f0`](https://github.com/YMGPwcca/phonelink-linux/commit/63a72f0bcff41bff7709bba81b8652ea0d8d0e5d) | `feat(clipboard): implement feature-state receive flow` |
| 2026-09-30 | [`e913aa2`](https://github.com/YMGPwcca/phonelink-linux/commit/e913aa20b029e6b810f435baec476542771b98f0) | `feat(msaep): implement PubSub envelope codec` |
| 2026-09-30 | [`d48c962`](https://github.com/YMGPwcca/phonelink-linux/commit/d48c962a25cff7d0a1a8136d84b3b105e34b06da) | `ci: add manual Go test workflow` |
| 2026-09-30 | [`d0ecd8a`](https://github.com/YMGPwcca/phonelink-linux/commit/d0ecd8abfd86956bf056ae814372d81c5303dfa0) | `feat(relay): add session-based Hub Relay messaging` |
| 2026-09-30 | [`02446be`](https://github.com/YMGPwcca/phonelink-linux/commit/02446beece497f38bcfd4b895f9d89d2e9b21a70) | `feat(clipboard): publish cloud updates through Context Publish` |
| 2026-09-30 | [`69acae8`](https://github.com/YMGPwcca/phonelink-linux/commit/69acae8e5eea909a8dd8fefaf728cd0389d156cc) | `fix(relay): separate DCG and Hub session identities` |

These commits establish the layer boundaries described in [`../protocol/transport.md`](../protocol/transport.md) and [`../protocol/clipboard.md`](../protocol/clipboard.md). They establish implementation contracts and tests, not live compatibility.

## Authentication, enrollment, and relay bootstrap

| Date | Commit | Title |
| --- | --- | --- |
| 2026-09-30 | [`2355864`](https://github.com/YMGPwcca/phonelink-linux/commit/23558641e5317f436a5dc431d842c40d408de98a) | `feat(auth): implement DCG identity cryptography` |
| 2026-09-30 | [`be8022a`](https://github.com/YMGPwcca/phonelink-linux/commit/be8022ab7006df275d03396d705e61e18a47796b) | `feat(auth): implement DCG authentication service` |
| 2026-09-30 | [`d96ded1`](https://github.com/YMGPwcca/phonelink-linux/commit/d96ded14709c53425fd857a727aab84c19993d98) | `feat(auth): add source-confirmed Microsoft account configuration` |
| 2026-09-30 | [`9d22dc4`](https://github.com/YMGPwcca/phonelink-linux/commit/9d22dc4b28b9e1cbf027fe7009523ff315ee5c85) | `feat(enrollment): implement DCG discovery and identity enrollment` |
| 2026-09-30 | [`de55b40`](https://github.com/YMGPwcca/phonelink-linux/commit/de55b40a0f6cbca328d6fa41402cb921898b4f1a) | `feat(dcg): apply source-confirmed service headers` |
| 2026-09-30 | [`b4f7164`](https://github.com/YMGPwcca/phonelink-linux/commit/b4f716434350d2daec4f0c8c0c14be3abe81a4cc) | `feat(auth): implement Microsoft device-code login` |
| 2026-09-30 | [`0e9e7bc`](https://github.com/YMGPwcca/phonelink-linux/commit/0e9e7bcb9ae7e367b3dfaafbabd469d92f5f88a7) | `feat(relay): track Hub Relay partner presence` |
| 2026-09-30 | [`59e4cf2`](https://github.com/YMGPwcca/phonelink-linux/commit/59e4cf2e4e46ac4ce3ca80839029cb5710ffb74e) | `feat(wake): implement signed DCG dispatcher wake` |
| 2026-09-30 | [`fcdecf0`](https://github.com/YMGPwcca/phonelink-linux/commit/fcdecf0cdfb2ba26720b5c83e7ff8784b8954237) | `feat(dcg): add CrossDevice application and peer discovery` |
| 2026-09-30 | [`472784e`](https://github.com/YMGPwcca/phonelink-linux/commit/472784eee2e1ef29a40aeb8beed07f327a4dad69) | `feat(auth): model Windows WAM token parameters` |
| 2026-09-30 | [`b323365`](https://github.com/YMGPwcca/phonelink-linux/commit/b323365f1aee72692c30ac2aca9ee1deedf0c7d7) | `feat(bootstrap): bridge Microsoft login to DCG identity` |
| 2026-09-30 | [`dcc4652`](https://github.com/YMGPwcca/phonelink-linux/commit/dcc4652ae59df1401737e321c743f41f675bbe7c) | `feat(enrollment): implement DCG device enrollment wire` |
| 2026-09-30 | [`1270bcb`](https://github.com/YMGPwcca/phonelink-linux/commit/1270bcba3c736a28756546f3dd3cff9df234d478) | `feat(auth): persist authentication and trust relationships` |
| 2026-09-30 | [`f17d282`](https://github.com/YMGPwcca/phonelink-linux/commit/f17d2820d5f53475b3d6075031b604e062b641c3) | `feat(bootstrap): synchronize trust and persist enrollment state` |
| 2026-09-30 | [`8803420`](https://github.com/YMGPwcca/phonelink-linux/commit/88034208f45ca89da3f769759b59551c0a302256) | `feat(relay): bootstrap CrossDevice account relay by shard` |
| 2026-09-30 | [`6e6d721`](https://github.com/YMGPwcca/phonelink-linux/commit/6e6d72112b45a7b4ce90cd26f39a605a5a2d181b) | `feat(bootstrap): resume persisted identity and orchestrate first run` |
| 2026-10-01 | [`cb616dc`](https://github.com/YMGPwcca/phonelink-linux/commit/cb616dc78a21021706ee5fdc07be4d1fe2a80a62) | `feat(cli): add production bootstrap probe` |

These commits are the dated implementation trail for the authentication and enrollment reference. PR #1 later summarized the same stages, but it predates the final live reports and is not proof of end-to-end status.

## Production-probe corrections and clipboard validation

| Date | Commit | Title |
| --- | --- | --- |
| 2026-10-01 | [`e6cb89f`](https://github.com/YMGPwcca/phonelink-linux/commit/e6cb89f0d1761e44847294c64876662113df335d) | `fix(signalr): accept binary handshake responses` |
| 2026-10-01 | [`c34da86`](https://github.com/YMGPwcca/phonelink-linux/commit/c34da86404a01ea00c601bef121c5227cca4a715) | `feat(cli): add peer wake and presence probe` |
| 2026-10-01 | [`7bc660e`](https://github.com/YMGPwcca/phonelink-linux/commit/7bc660e77a09f85d87b81329d3883837f0221f98) | `feat(session): implement PLATFORM SessionValidation` |
| 2026-10-01 | [`d737b21`](https://github.com/YMGPwcca/phonelink-linux/commit/d737b21442b9f2ac67afdacb3008930a540679f3) | `fix(relay): use source-confirmed Hub transport message types` |
| 2026-10-01 | [`002ea31`](https://github.com/YMGPwcca/phonelink-linux/commit/002ea31398ea156221f1fb85719b6dfcd83c47a9) | `feat(relay): await Hub completions for outgoing sends` |
| 2026-10-01 | [`abb99ca`](https://github.com/YMGPwcca/phonelink-linux/commit/abb99ca25f927e12367ef68b619ddba2fe8ec79d) | `fix(wake): flush partner presence before peer wake` |
| 2026-10-01 | [`4652bb4`](https://github.com/YMGPwcca/phonelink-linux/commit/4652bb4729b187e3f9efae4d33ba0bc24bcfd080) | `fix(relay): generate valid Hub Relay trace contexts` |
| 2026-10-01 | [`4b4c093`](https://github.com/YMGPwcca/phonelink-linux/commit/4b4c09312d6974f9b0df8c77141349b8d973d7cf) | `feat(clipboard): add Context Publish production probe` |
| 2026-10-01 | [`ee33183`](https://github.com/YMGPwcca/phonelink-linux/commit/ee3318368e1f8e73b683b2912ab59a9ef16d39d5) | `feat(clipboard): negotiate STATUS to CONTENT in Context probe` |
| 2026-10-01 | [`77844a5`](https://github.com/YMGPwcca/phonelink-linux/commit/77844a5b1148c14ef585a396c2af55563beff90a) | `feat(clipboard): validate explicit text delivery through Context probe` |
| 2026-10-01 | [`8da85c7`](https://github.com/YMGPwcca/phonelink-linux/commit/8da85c778d5f072a92336c3e4daa93606551b035) | `feat(clipboard): preserve published text by correlation` |
| 2026-10-01 | [`1b2e194`](https://github.com/YMGPwcca/phonelink-linux/commit/1b2e1947c67cdf1bf12846001b70605f352ee9c8) | `feat(clipboard): add continuous native Linux clipboard sync` |
| 2026-10-01 | [`9a039c6`](https://github.com/YMGPwcca/phonelink-linux/commit/9a039c634f87d4ff05fff0abfa31e100e040341e) | `fix(clipboard): suppress reflected updates and remote races` |
| 2026-10-01 | [`9b55277`](https://github.com/YMGPwcca/phonelink-linux/commit/9b552776b996f91ab9285305cd10f6feb912b7dd) | `fix(clipboard): harden Wayland writes and echo normalization` |
| 2026-10-01 | [`9cf0521`](https://github.com/YMGPwcca/phonelink-linux/commit/9cf052198995621a21d9658fbd397be1266bdcc1) | `fix(clipboard): serialize clipboard and relay send ordering` |
| 2026-10-01 | [`a37e286`](https://github.com/YMGPwcca/phonelink-linux/commit/a37e286c85e451ed4bbda59074d62f730b4eb120) | `fix(clipboard): harden stale snapshot and queued-send handling` |
| 2026-10-01 | [`6e50c69`](https://github.com/YMGPwcca/phonelink-linux/commit/6e50c69f29fb853cbb776e4c62c9fba5f2bc9b9c) | `fix(relay): surface Hub read-loop failures immediately` |
| 2026-10-01 | [`acfdf99`](https://github.com/YMGPwcca/phonelink-linux/commit/acfdf99e6dd17de2d4a238dba9ca841a69110b51) | `fix(clipboard): scope peer traffic and validate response correlations` |

These commits explain the observed sequence from bootstrap through SessionValidation, tag-9 publication, STATUS/CONTENT, correlation snapshots, and continuous native sync. Commit dates show when code changed; the live validation record identifies which changes were exercised.

## Modular runtime and service lifecycle

| Date | Commit | Title |
| --- | --- | --- |
| 2026-10-01 | [`af095a7`](https://github.com/YMGPwcca/phonelink-linux/commit/af095a76d05a609c307f764ebf38f8c76ca7d074) | `feat(runtime): add feature manifests and lifecycle kernel` |
| 2026-10-01 | [`1a04f7c`](https://github.com/YMGPwcca/phonelink-linux/commit/1a04f7c5c9943cbb6832bcd16b5fd7bc8e37ffef) | `feat(runtime): add shared Phone Link host` |
| 2026-10-01 | [`bc5576f`](https://github.com/YMGPwcca/phonelink-linux/commit/bc5576f549d69eaaf1c129a974cecf6f0929e46d) | `feat(clipboard): implement modular clipboard feature` |
| 2026-10-01 | [`5c0e582`](https://github.com/YMGPwcca/phonelink-linux/commit/5c0e582e889588dc2bfedf4368e36451b3e1f9eb) | `feat(cli): add modular feature runtime and CRUD` |
| 2026-10-01 | [`8037b44`](https://github.com/YMGPwcca/phonelink-linux/commit/8037b44a0ba44b0308dd268860cdad7949c45ed4) | `fix(clipboard): unify clipboard generation ownership` |
| 2026-10-01 | [`f1ff937`](https://github.com/YMGPwcca/phonelink-linux/commit/f1ff9372ffda2cf74746424bdcf8071cf8a3566c) | `fix(runtime): harden feature CRUD and lifecycle epochs` |
| 2026-10-01 | [`e75d529`](https://github.com/YMGPwcca/phonelink-linux/commit/e75d5299f5b339cd04014849195d424a961b9fa8) | `feat(runtime): route feature traffic through scoped endpoints` |
| 2026-10-01 | [`5d0d5c1`](https://github.com/YMGPwcca/phonelink-linux/commit/5d0d5c154fbfdb0fb18c988ca75a8a676855b82f) | `fix(clipboard): enforce generation-aware remote conflict handling` |
| 2026-10-01 | [`eeb2d57`](https://github.com/YMGPwcca/phonelink-linux/commit/eeb2d575965157ceea9a554700eb5584a99a30a) | `fix(runtime): harden persistence and dependency teardown` |
| 2026-10-01 | [`ce7b6a5`](https://github.com/YMGPwcca/phonelink-linux/commit/ce7b6a530d35b46803749290412864f01af870b6) | `docs(runtime): document modular feature architecture` |
| 2026-10-01 | [`eb61ebe`](https://github.com/YMGPwcca/phonelink-linux/commit/eb61ebe4f0d31e9e2ec15ed66828297f2594b311) | `fix(runtime): harden startup rollback and desired state isolation` |
| 2026-10-01 | [`6d47ce0`](https://github.com/YMGPwcca/phonelink-linux/commit/6d47ce08f0f021a1673a02029b4f5c418b272861) | `fix(runtime): harden modular lifecycle and feature routing` |
| 2026-10-01 | [`1a41a38`](https://github.com/YMGPwcca/phonelink-linux/commit/1a41a38fe58d76aefe8dc9a3624b19080539e9f1) | `feat(control): add live feature control plane` |
| 2026-10-01 | [`3d64a87`](https://github.com/YMGPwcca/phonelink-linux/commit/3d64a87280d747968d8c9655144917b5a636034c) | `fix(control): harden runtime socket ownership and live CRUD` |
| 2026-10-01 | [`3c79498`](https://github.com/YMGPwcca/phonelink-linux/commit/3c79498277dc84473091b54fad6a875808f67e7c) | `feat(clipboard): add event-driven Wayland observation` |
| 2026-10-01 | [`5ba71ec`](https://github.com/YMGPwcca/phonelink-linux/commit/5ba71ec96c7bafb2f7a1827693da7005e757c221) | `feat(service): add resilient systemd user service lifecycle` |
| 2026-10-01 | [`e51728b`](https://github.com/YMGPwcca/phonelink-linux/commit/e51728b12a148061c8076c67200ee4a8f366e423) | `Merge PR #1: implement Phone Link clipboard protocol and transport` |

The final merge commit is the main baseline for this documentation rewrite. The README at that commit reports historical live feature CRUD, generation ordering, event-driven Wayland operation, and systemd lifecycle behavior. Those reports are not inferred solely from commit titles.

## PR #1 provenance

GitHub PR #1 was merged into main as `e51728b`. Its body describes the protocol stack, authentication/bootstrap stages, early validation boundaries, and fixes for session identity, normal platform routing, binary handshake handling, and trace context. The body was written before later commits and still lists the native backend and live bootstrap as pending. Later main commits and the baseline README supersede those pending statements for historical status, while the PR body remains useful as dated design context. PR #1 has no review comments or attached raw packet/log artifacts in the repository record.

## Rich clipboard development branch

The following commits are on `feature/clipboard-content`; this documentation describes that branch and does not assert that it has already been merged into main. Dates in this table use UTC commit timestamps.

| Date (UTC) | Commit | Result |
| --- | --- | --- |
| 2026-10-02 | [`ecc54d6`](https://github.com/YMGPwcca/linkmyphone/commit/ecc54d65cc421f192fe9732a092463a526c5a331) | Typed text/HTML/image transfer, HTML fallback offers, immutable bounded snapshots, MIME polling and content tests. |
| 2026-10-02 | [`2670f26`](https://github.com/YMGPwcca/linkmyphone/commit/2670f2656b9635f5359cd4b2f00945f4a9ab1f0c) | BMP decoding, larger encoded phone images, native MIME targets and third-party notices. |
| 2026-10-03 | [`7f999f3`](https://github.com/YMGPwcca/linkmyphone/commit/7f999f3b3b5ca7a2857a49e5a6b0ea3b2016f088) | Preserve incoming dimensions and full desktop image hashes; enforce the PNG budget only on outbound snapshots and live CONTENT fallback. |
| 2026-10-03 | [`98a9e5c`](https://github.com/YMGPwcca/linkmyphone/commit/98a9e5c17a280da3d840b12d61fb50c07fd23931) | Run CI only when main is updated. |

Owner-reported live results on 2026-10-03 support marking Rich text & HTML clipboard and Image clipboard Supported. See [validation](validation.md#clipboard-validation-2026-10-03) for paste results, incoming dimension equality and the outbound comparison; commit titles alone are not compatibility evidence.
