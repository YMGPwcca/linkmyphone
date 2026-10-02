# Protocol research

[Documentation index](../README.md)

Use this directory according to the question you have:

| If you need to… | Start with… |
| --- | --- |
| Follow one message from sign-in to clipboard | [Authentication and bootstrap](../protocol/authentication.md), then [Transport and framing](../protocol/transport.md), then [Clipboard protocol](../protocol/clipboard.md) |
| Check an implementation contract | [Transport and framing](../protocol/transport.md) or [Clipboard protocol](../protocol/clipboard.md) |
| Understand why a behavior changed | [Findings](findings.md), then the linked [development history](history.md) |
| Reproduce a safe investigation | [Method and provenance](method.md), then [Validation](validation.md) |
| Separate source, tests, commits, and live reports | [Method and provenance](method.md#what-the-repository-preserves) |
| See what was actually reported on a phone and desktop | [Validation](validation.md#historical-stage-results) |

The [architecture overview](../architecture/overview.md) maps Linux packages to these layers. Use the [glossary](../reference/glossary.md) when an identifier or protocol term is unfamiliar.

## Follow a message

### Wire path

1. [Authentication and bootstrap](../protocol/authentication.md) covers account tokens, persistent identities, enrollment, trust, relay assignment, wake, and `/SessionValidation`.
2. [Transport and framing](../protocol/transport.md) covers WebSocket messages, SignalR invocations, Hub Relay packets, DCG fragments, and PLATFORM requests.
3. [Clipboard protocol](../protocol/clipboard.md) covers `/Context/Publish`, `/DeviceResourceManager`, `/internal/response`, clipboard payloads, and cross-device ordering.

## Follow the evidence

1. [Method and provenance](method.md) explains what each evidence class establishes, how to choose the smallest probe, and what artifacts are absent.
2. [Findings](findings.md) groups the assumptions that failed, the observed or inferred result, the implementation change, and its source evidence.
3. [Development history](history.md) lists every dated commit in implementation order.
4. [Validation](validation.md) records historical stage outcomes, commands, limits, safety boundaries, and open gaps.

## Evidence boundaries

The repository contains the Go implementation, deterministic tests, commit history, and written reports of production checks. It does not contain the decompiled Windows source corpus, original protocol definitions, raw production capture logs, or a complete research-session transcript.

A source reference proves what the Linux implementation does. A test proves behavior within its test setup. Historical live reports describe the recorded S23, Wayland, and systemd observations; they do not establish compatibility with every phone, compositor, or service deployment. Commit dates establish development order, not the time or success of a live experiment.

For a new investigation, record the environment and evidence before drawing a compatibility conclusion. [Research method](method.md) gives the capture and redaction procedure. The [privacy guide](../operations/privacy-and-state.md) identifies material that must stay out of issues and pull requests.
