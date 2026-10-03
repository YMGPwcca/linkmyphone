# Notification sync investigation

[Research index](README.md)

Research date: 2026-10-03. Repository baseline: `299248d87cc383e57ebce4b6c11048893d83ddb2`, the session-resilience merge on `main`. Work branch: `feature/notification-sync`.

**Status: source investigation only.** No notification implementation, production-service probe, phone interaction, or Windows notification experiment was performed. Findings below describe the supplied source snapshots; they do not establish notification compatibility of LinkMyPhone's current enrollment.

## Main findings

The Android snapshot contains two separate notification contracts. Phone Link's full contract pushes changes and accepts dismissal, resync and notification actions. The CrossDevice contract returns a protobuf snapshot through DeviceResourceManager and defines only GET. Their shared `/notifications` spelling does not make their envelopes interchangeable.

The main compatibility question is recipient selection. Full notification publication starts from Phone Link clients (`PL`). Connection sharing can redirect a paired PL client's traffic to a CrossDevice client (`WEA`) for the same `distinguishedDeviceId`. LinkMyPhone currently enrolls as WEA; neither standalone WEA push support nor a suitable shared PL association has been demonstrated.

The Windows archive supplies common transport/SDK code, but no notification feature assembly or UI implementation was found. Android-side behavior is much better evidenced than Windows toast creation, storage, suppression, or action handling.

## Two contracts

| Concern | Phone Link notifications | CrossDevice notification snapshot |
| --- | --- | --- |
| DCG transport kind | APP; Hub Relay value `0` | PLATFORM; Hub Relay value `1` |
| Phone-to-PC publication | APP `_route=/legacy/phonecontent`, payload `contentType=notifications` | No notification push contract found in the inspected handler/schema |
| PC-to-phone request | APP `_route=/notifications` | PLATFORM `_route=/DeviceResourceManager`, inner `resource_path=/notifications` |
| Payload encoding | Typed PBValueSet map; notification bodies are JSON strings inside arrays | DeviceResourceManager protobuf containing notification-specific protobuf bytes |
| Available behavior | Posted/updated, removed, existing-item reconciliation; dismiss, clear, launch/action/reply | GET current publishable notifications |
| Icons and actions | Included in the full notification JSON | Absent from this `NotificationInfo` schema |
| Permission failure | AppService result `7` | Notification response status `3` |

Sources: A1-A8, A12-A14 below. The two numeric transport values are Hub Relay values, not the separate protobuf transport enum. See the [transport reference](../protocol/transport.md#dcg-envelope).

## Full Phone Link delivery

`PhoneNotificationsListenerService` extends Android `NotificationListenerService`. Android notification access allows it to observe other apps' notifications. The manifest's `BIND_NOTIFICATION_LISTENER_SERVICE` declaration is distinct from `POST_NOTIFICATIONS`, which concerns posting the app's own notifications.

The source path is:

1. `onNotificationPosted` filters a `StatusBarNotification`, caches any reply action, and queues operation `1` with its notification data.
2. `onNotificationRemoved` queues operation `2` with its key and no notification body.
3. Listener queues flush after a 500 ms delay. An experiment optionally coalesces changes by key, keeping the last operation.
4. Listener-to-main Messenger IPC applies package preferences to body-bearing incremental operations. Bodyless removal operations pass this filter, allowing old PC entries to be removed.
5. `PhoneNotificationsMessageBuilder` creates the typed payload. `SyncExecutor` selects active recipients and sends it through `RemoteAppClient` to `/legacy/phonecontent`.
6. The app messaging sender marks these messages APP. The receiver dispatches APP messages by their `_route`; the PLATFORM DeviceResourceManager handler is a separate entry point.

The listener's `131072` byte batching argument belongs to listener-to-main IPC. Full-sync transactions accumulate operations before publication; this value is not established as a network packet or notification-payload limit. Ordinary event publication also requires the Messenger connection and Agents registration state. A successful PLATFORM `/SessionValidation` alone does not establish those app-feature prerequisites. Sources: A1-A4, A8.

### Typed payload and notification data

| Entry | Type or meaning |
| --- | --- |
| `contentType` | String `notifications` |
| `contractVersion` | Double `3.99` in this snapshot |
| `incremental` | Int32 `1` from the CONTENT_ONLY builder, including existing-item publications |
| `notificationKeys` | String array |
| `operations` | Int32 array |
| `notifications` | String array of notification JSON; removal bodies serialize as empty strings |
| `correlationVector` | Application correlation value, separate from transport request IDs |
| Other base entries | Installation ID, dedupe ID, retry count, display name and device metadata |

The three operation arrays have matching lengths and matching positions. `ValueSetSerializerUtility` converts null string-array members to `""`; a removal entry must not be decoded as required JSON. PBValueSet preserves scalar and array types, so a dictionary of JSON strings is insufficient. Sources: A3, A4.

`NotificationItem` exports identity (`id`, `key`, `groupKey`, `tag`, package), app name, clearable/group/ongoing flags, category, ranking and time information, plus available text fields (`title`, `text`, `bigText`, `subText`, `conversationTitle`, `textLines`, and others). MessagingStyle data can include message/sender arrays. The builder adds base64 `largeIcon` and `smallIcon`; its bitmap helper uses JPEG quality 97 and may resize the bitmap first. Do not assume PNG or preserved transparency.

Each exported action includes `actionName`, `isActionInlineReply` and `actionIndex`. The index refers to the original Android `Notification.actions` array; skipped actions must not cause reindexing. The full JSON and GET endpoint also classify notifications differently: the full path uses the default SMS package for class `1`, whereas GET uses MessagingStyle. A common numeric value does not prove identical classification. Sources: A5, A13.

### Operations and responses

| Direction | Operation | Observed handling |
| --- | ---: | --- |
| Phone → PC | `1` | Posted notification, including a later update for the same key |
| Phone → PC | `2` | Remove by notification key |
| Phone → PC | `3` | Existing notification returned during reconciliation |
| PC → phone | `2` | Dismiss `key`; listener calls `cancelNotification(key)` |
| PC → phone | `3` | Request reconciliation with `notificationKeys` and Int64-array `postTimes` |
| PC → phone | `4` | Clear the supplied `notificationKeys`; an empty/missing list reaches `cancelAllNotifications()` |
| PC → phone | `5` | Launch a notification, invoke `actionIndex`, or submit `inlineReplyMessage` |

Requests use `contentType=notifications`. APP request headers carry `_requestId`; responses use `_route=/internal/response` and `_originalRequestId`. AppService response maps include `result` (`0` success, `1` failure, `7` notification permission needed). The handler, rather than the incomplete operation-name helper, establishes operation `5`. Media control is handled separately by the audio-command route; the helper's name for operation `6` does not establish an additional operation supported by this notification handler. Sources: A6, A8.

Dismissal and inline-reply requests can receive a success response immediately after dispatch to the listener. This acknowledges that stage, not the final disappearance of the notification or delivery of a message to its recipient. For a future client, confirm dismissal through the later remove event or reconciliation. Avoid interpreting transport ACKs as action outcomes.

Inline reply uses the selected Android action's free-form `RemoteInput`, adds the reply to an Intent, and sends its `PendingIntent`. It is not a separate SMS or chat-service API. A bounded reply-action cache can retain a removed notification's action (default five-minute TTL, 100 entries), but a cached PendingIntent may still fail. General actions and opening notifications have additional Android launch/permission behavior; their success cannot be inferred from inline-reply support. Sources: A1, A5-A7.

### Reconciliation and filtering

The `/connect` handler invokes `PhoneNotificationConnectionSyncCoordinator`. Operation `3`, PC keys and `postTimes` trigger a full-sync transaction; the coordinator requires a `postTimes` array. The same operation is explicitly available through `/notifications`.

The listener collects current publishable notifications, passes them and disabled-package settings to `PhoneNotificationSyncPlanner`, emits operation `3` for returned existing items and operation `2` for stale PC keys, then completes the transaction. If the listener is not ready, it sends an empty completion and explicitly avoids deleting PC notifications. Missing/short post-time arrays are checked; they must not be treated as an authoritative empty phone state. Sources: A1, A9.

**Unresolved predicate:** JADX shows an empty conditional in the planner's same-key/post-time branch, followed by unconditional insertion. The GET handler has a similar empty conditional. Stale-key removal is readable, but skipping unchanged notifications and the precise GET delta-filter behavior require bytecode inspection or independent captures. Do not implement these conditions by guessing from the decompiled Java.

Filtering occurs at several boundaries:

- The publish filter can exclude progress indicators, grouping summaries, unsupported custom layouts, the app's own internal notifications and certain connection notifications. User/profile and media settings also affect selection; supported media and some foreground MessagingStyle notifications have exceptions.
- Full publication applies package-level notification preferences; unknown packages default to enabled in the inspected app filter. Reconciliation receives the disabled-package list.
- Snapshot GET uses the listener's publishable list. Its inspected handler does not invoke the full path's package-preference filter, so identical per-app mute behavior is not established.

These rules are not a guarantee to mirror every entry in Android's notification shade. Source: A10.

## CrossDevice GET contract

The outer DeviceResourceManager message uses `request_type=GET` (`1`), `resource_path=/notifications`, and bytes containing a `NotificationsRequestMessage`. That inner request has `type=GET` (`1`), repeated `keys` and repeated Int64 `post_times`. The deprecated resource-type enum does not define a notification-specific value; the path selects the handler.

The response has a separate DeviceResourceManager wrapper and then `NotificationsResponseMessage`: `status` at field `1`, repeated `NotificationInfo` at field `2`. Notification statuses are `0` unspecified, `1` OK, `2` invalid request, `3` permission not granted, `4` no notification and `5` unknown error. A wrapper response such as handler-not-registered is a different result from one of these inner statuses.

An empty `keys` request takes the straightforward snapshot path and sorts items by descending post time. Returned fields are key, title, text, big text, app/package name, post time, timestamp, category, clearable flag, subtext, conversation title and notification class. There are no icons, action descriptors, dismissal commands or subscription operations in this schema. Nonempty-key delta semantics remain unresolved as noted above. Sources: A12-A14.

`GET_NOTIFICATIONS` capability advertisement is gated by `ENABLE_GET_NOTIFICATIONS_CROSS_DEVICE`. This establishes an advertisement condition, not by itself a runtime access check or proof that the handler is reachable for the current account/build. Also, the listener's retriever returns an empty list when unavailable/not ready; `NO_NOTIFICATION` must not automatically be interpreted as proof that all cached phone notifications were dismissed. Sources: A1, A13, A15.

## Recipient identity and connection sharing

`SyncExecutor.executeMulticastAsync` asks for `getActiveRemoteApps(PL)`. Without connection sharing, this selects active PL remote apps. With sharing, `RemoteDeviceManager` starts from PL remote apps that are active considering sharing, then can substitute a WEA remote app with the same `distinguishedDeviceId`. `RemoteAppClient.sendMessageWithMessage` also resolves the shared target before sending. Source: A11.

This leaves two cases to distinguish experimentally: a genuine PL peer/session, and PL traffic carried through its associated WEA connection. WEA alone is not shown to be a full-notification recipient. Conversely, the source does not justify concluding that a second OAuth login or changing the app ID is always necessary. `RemoteAppUtil` reads `clientType` and device association from trusted-peer metadata; changing a request body is not proof of changed enrollment classification.

LinkMyPhone's [first-run bootstrap](../../bootstrap/first_run.go) calls `MetadataForClipboardPC`, which currently supplies `ClientType=WEA` and only the `CLIPBOARD` capability. [Client headers](../../dcgheaders/profile.go) use `MicrosoftWindows.CrossDevice_cw5n1h2txyewy`. Both the trusted metadata/association and the APP `/connect` contract need investigation before assuming full push works through this session. No enrollment identity or persisted credentials were changed in this work.

## Windows evidence and transport cautions

The supplied Windows code confirms the shared binary transport envelope, application request correlation, PLATFORM `/DeviceResourceManager` plumbing and APP `/DeviceProxyClient/TransportMiddleware` reception. Its `OutgoingPlatformKvpMessage` serializes PLATFORM key-value messages as UTF-8 JSON; this is distinct from Android's full notification APP PBValueSet payload. Sources: W1-W3.

There is a source discrepancy worth preserving before codec reuse: Android's `BinaryMessageSerializationStrategy` calculates the header byte length without the four-byte header count; the supplied Windows implementation writes a stream length that includes it. LinkMyPhone's current [PLATFORM codec](../../protocol/platform/message.go) uses the Android-style length. This observation is not a live interoperability failure and no codec was changed. Compare independent APP/PLATFORM samples before assuming every producer uses exactly the same length convention or relaxing bounds checks. Sources: A16, W1.

No Phone Link notification feature implementation, `CrossDevice.Notifications` implementation, notification cache, or Windows toast renderer was found among the supplied Windows assemblies. Shared wake/push-notification classes are transport wake infrastructure, not evidence of Android notification mirroring UI. Duplicate source directories in the archive do not provide an independent second implementation. Windows UI details remain unverified.

Microsoft's public support documentation describes enabling notification access, per-app selection, dismissal on either device and actions/replies when the app supports them. It also documents restrictions for sensitive notifications on Android 15+. These public behaviors support the intended product experience, but do not specify the private wire contracts or prove Linux compatibility; see the references below.

## Implications for LinkMyPhone

| Existing component | Reuse or remaining work |
| --- | --- |
| Account enrollment, trust, wake and relay | Reuse infrastructure; verify the notification recipient/session prerequisites separately |
| DCG APP/PLATFORM transport types and reassembly | Already modeled; new feature traffic still needs the correct type and envelope |
| PLATFORM framing and `NewDeviceResourceRequest` | Existing constructor; notification and inner DeviceResourceManager protobuf codecs are not implemented |
| `phonehost.Session.Subscribe` and shared router | Existing feature-scoped receive entry point with bounded queues |
| Session recovery and module lifecycle | Existing restart/epoch framework; notification reconciliation and pending-action policy remain feature work |
| Feature catalog | Clipboard only at the baseline; no notification module, state store, native desktop backend or reply bridge |
| Full notification APP contract | Needs PBValueSet handling, request/response dispatch, verified connection setup and notification-specific state |

**Recommended order for later work, not an implementation plan already executed:** first establish the reachable contract and recipient identity, then independently validate snapshot/delta bytes, then state reconciliation, then desktop display, then dismissal and reply. Polling GET could provide a limited snapshot experiment; it must not be presented as full Phone Link synchronization or as a contract offering remote actions.

A future module should own notification state and use the shared Session subscription rather than reading the relay channel directly or adding notification rules to the kernel/clipboard feature. Scope keys by phone and Android notification key, use post time as reconciliation metadata, keep action indices unchanged, and distinguish existing-item refresh from a new-alert policy. Those are design recommendations; Windows's exact alert policy is not established here.

## Follow-up validation questions

| Question | Evidence needed |
| --- | --- |
| Is snapshot GET available for the current WEA peer? | Capability advertisement, correlated DRM wrapper and notification status, with permission granted/denied |
| Does the current client qualify for full push? | Trusted peer client type, redacted device association, sharing setting and an independently observed APP notification message |
| What initializes the app notification session? | Capture or bytecode-confirmed minimal `/connect` contract and Agents registration prerequisites |
| How are unchanged keys filtered? | Inspect the APK bytecode for the two empty JADX predicates; confirm with same-key/same-post-time and updated-post-time samples |
| Which header-length convention arrives? | Redacted independent APP and PLATFORM frames from the exact builds |
| How do state updates and reconnect behave? | Post/update/remove one harmless notification; reconcile after disconnect, then with the listener unavailable |
| Which app filters apply to GET and full sync? | Same app enabled/disabled in each path, plus group/progress/media examples |
| Are dismiss and replies complete? | Correlate request response, subsequent removal or app reply result; distinguish cached-action, expired and canceled-intent cases |
| What does Windows render? | Notification-specific assemblies or a direct Windows test of initial sync, updates, dismiss and actions |

No new tests were run against devices or Microsoft services. Use the existing [research method](method.md) to record environment and acknowledgement boundaries when these experiments are authorized. Keep credentials, private notification contents and raw captures outside the repository.

## Provenance and source map

The user supplied both archives. SHA-256 identifies the ZIP itself, including its supplied extraction contents; it does not attest the extraction tool's correctness. This investigation read selected JADX Java, resource schemas/manifest and Windows C# files. The Android archive also contains APKs; the original bytecode was not disassembled in this pass. No third-party source corpus or binary is imported into this repository.

| Archive | SHA-256 | Version evidence |
| --- | --- | --- |
| `ltw-baseline-20260929-203827(1).zip` | `8022cbc1f2c1b60b17b8fc30405d3d5339e61c31fe2597383973179de40676ce` | Inspected Microsoft-Active manifest: package `com.microsoft.appmanager`, version `1.26082.130.0`, code `8200516` |
| `phonelink-winrev(1).zip` | `ad55af430fb0d1760e1d8793a349608a92f6797633daa65d2bcc48c64704d2e5` | Inspected `YourPhone.YPP` AssemblyInfo: `0.26072.49.0`; informational `0.26072.49+24015e6fff74d5c89a33e3a8d3fd109a08ba5000` |

The Android archive includes other Microsoft/Samsung package snapshots; the claims here use `jadx/Microsoft-Active`, not an assertion that every bundled variant behaves identically. The inspected Windows assembly version is not assumed to be the Phone Link Store package version.

Android source paths below are relative to `ltw-baseline-20260929-203827/jadx/Microsoft-Active/sources/com/microsoft/`; Android resources are relative to its `resources/`. Windows paths use the archive's `YourPhone.YPP/` directory.

| ID | Source locations and useful symbols |
| --- | --- |
| A1 | `mmx/agents/PhoneNotificationsListenerService.java`: `onNotificationPosted`, `onNotificationRemoved`, `getAllActiveNotifications`, `sendFullSyncMessage`, `triggerInlineAction`, `sendInlineReply`, queue initialization; resource `AndroidManifest.xml` |
| A2 | `mmx/agents/notifications/messenger/PhoneNotificationsSender.java`, `PhoneNotificationsListenerToMainMessageHandler.java`, `NotificationOperationCoalescer.java` |
| A3 | `mmx/agents/notifications/messenger/PhoneNotificationsMessageBuilder.java`: `createNotificationsPayloadMap`; `mmx/agents/sync/KvpMessageHelpers.java`: `getBaseMessageMap`, CONTENT_ONLY handling |
| A4 | `mmx/agents/ypp/valueset/PayloadHelper.java`, `ValueSetSerializerUtility.java`: String-array null handling; resource `value_set/valueset.proto`; `mmx/agents/sync/AppServiceMessageToOutgoingMessageAdapter.java` |
| A5 | `mmx/agents/notifications/models/NotificationItem.java`: `toJson`, action extraction and classification; `appmanager/utils/FileUtils.java`: `convertBitmapToByteArray` |
| A6 | `mmx/agents/notifications/requestsHandlers/PhoneNotificationRequestHandler.java`: `tryProcessRequest`, `performNotificationLaunch`; `mmx/agents/notifications/messenger/PhoneNotificationsMainToListenerMessageHandler.java`; `mmx/agents/AppServiceProviderHelpers.java` |
| A7 | `mmx/agents/notifications/NotificationReplyActionCache.java`; `mmx/agents/notifications/models/PhoneNotificationOperationType.java` |
| A8 | `appmanager/DaggerMainApplication_HiltComponents_SingletonC.java`: route registration; `mmx/agents/transport/MessageRouter.java`, `OutgoingRequest.java`, `OutgoingResponse.java`; `mmx/agents/ypp/signalr/transport/MessageSender.java`, `MessageReceiver.java` |
| A9 | `mmx/agents/ConnectionRequestHandler.java`; `mmx/agents/notifications/PhoneNotificationConnectionSyncCoordinator.java`: `processConnectionPayload`; `PhoneNotificationSyncPlanner.java`: `plan` |
| A10 | `mmx/agents/notifications/PhoneNotificationsPublishFilter.java`, `PhoneNotificationsAppFilter.java`; A1 and A2 for filter application |
| A11 | `mmx/agents/sync/SyncExecutor.java`: `executeMulticastAsync`; `mmx/agents/remotedevice/RemoteDeviceManager.java`: `getActiveRemoteApps`, shared-device lookup; `ConnectionSharingManager.java`; `mmx/agents/remoteapp/RemoteAppClient.java`; `mmx/agents/RemoteAppUtil.java` |
| A12 | Resource `notification/v1/notification.proto`: GET request, response status and `NotificationInfo` field definitions |
| A13 | `mmx/agents/notifications/platform/NotificationResourceRequestHandler.java`: `getResourcePath`, `validateRequest`, `handleNotificationsRequest`; `mmx/agents/notifications/NotificationManager.java` |
| A14 | Resource `platform_sdk/v1/device_resource_manager.proto`; `mmx/agents/ypp/ambientexperience/deviceresource/signalr/SignalRDeviceResourceManager.java` |
| A15 | `appmanager/notification/GetNotificationsCapabilityProvider.java`; `mmx/agents/ypp/devicemanagement/provider/DeviceCapabilityModule.java` |
| A16 | `mmx/agents/ypp/transport/chunking/BinaryMessageSerializationStrategy.java`, `BinaryMessageDeserializationStrategy.java` |
| W1 | `YourPhone.YPP.Transport.Chunking/BinaryMessageSerializationStrategy.cs`; `YourPhone.YPP.Transport.PlatformMessaging/PlatformMessageToOutgoingMessageAdapter.cs`, `OutgoingPlatformKvpMessage.cs`, `PlatformMessageManager.cs` |
| W2 | `YourPhone.YPP.DeviceResourceManagement.SignalR/SignalRDeviceResourceProvider.cs` |
| W3 | `YourPhone.YPP.Transport.DeviceProxyMessaging/DeviceProxyMessageReceiver.cs`; `Properties/AssemblyInfo.cs` |

Microsoft public references, inspected on the research date:

- [Set up notifications in Phone Link](https://support.microsoft.com/en-gb/windows/apps/phonelink/setting-up-notifications-in-the-phone-link).
- [View and manage mobile notifications on your PC](https://support.microsoft.com/en-au/windows/apps/view-and-manage-mobile-notifications-on-your-pc).
- [Troubleshoot notifications in Phone Link](https://support.microsoft.com/en-us/windows/apps/phonelink/troubleshooting-notifications-in-the-phone-link).
