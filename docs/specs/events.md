# sock v1 domain events

Pass 9: feature flags add no events; the `feature_flags` notification is cache invalidation ([feature flags](feature-flags.md)). Pass 8: History transfer emits `HistoryTransferChanged`, internal to its own context ([history transfer](history-transfer.md)). Pass 7. Sign-in from the CLI and the operator console's Apple sign-in add no events. Sock Pro emits `ProEntitlementChanged` and suspension `AccountSuspensionChanged`, so Messaging can tell which puppets are active ([Sock Pro billing](sock-pro-billing.md)); Identity's own gate reads its tables directly. Machine keys, connector grants and the support puppet add no events either; they change which transitions emit the existing ones (the table below, and Machine keys, connector grants and the support puppet). Archiving or resuming a puppet emits `PuppetArchiveChanged`. An operator erasure emits the existing `AccountErased`. Triage adds none: it polls `ListReports`, and there are no operator events. Server-internal: no client sees these. Payloads are protobuf messages defined in the `server` repo (`proto/sockevents/v1`), stored as `BYTEA` in a transactional outbox written in the same transaction as the state change (the server's data model).

## Delivery mechanics (all events)

- Written to the emitting context's outbox table in the emitting transaction. No event without its state change, no state change without its event.
- Moderation outbox: consumed like the Messaging outbox (leased, concurrent, unordered). `ReportFiled` (alert) and `ReportResolved` (suspension) do not depend on each other's order and both handlers are idempotent, so a failing alert must never hold a suspension behind it.
- Identity outbox: consumed by one leader (session advisory lock) using `SELECT … FOR UPDATE`, oldest first; the handler runs in the claim transaction and the row is deleted when it commits. Every Identity outbox insert takes the transaction advisory lock `identity.outbox` first, held until commit, and reads `created_at` under it as `greatest(clock_timestamp(), last + 1 microsecond)`, where `last` is the previous value, kept in the one-row `identity.outbox_clock` updated under the same lock (the outbox rows are deleted, so they cannot hold it), so `created_at` strictly increases; taking the lock and inserting the row are the transaction's last statements (for `Device.Issue`, the device's `created_at` is set to the same timestamp just before the insert). Writers wait on each other only from insert to commit. So row ids and `created_at` rise in commit order: a row visible to the consumer has no uncommitted row with a lower id or an earlier `created_at` behind it. (A sequence id alone is assigned at insert, not commit: without the lock an earlier id can commit after a later one was consumed.)
- Messaging outbox: each replica leases up to 8 ready rows in one short statement (`FOR UPDATE SKIP LOCKED`, setting `available_at` to now plus a 60s lease and counting the attempt), runs handlers concurrently outside any transaction, then deletes, reschedules or dead-letters each row guarded on its lease value. A row whose holder dies is claimed again when the lease expires.
- At-least-once. Every handler is idempotent (upsert or delete-if-exists).
- A handler failure is retried with exponential backoff (1s doubling, cap 5 min) by bumping the row's `available_at`. After 20 attempts (for the Messaging outbox an attempt is a claim, so an expired lease counts) the row moves to the dead-letter table and logs. Dead letters are swept 30 days after they die (Deadlines), except erasure events (`AccountErased`, for a human or a puppet): those are never swept, never dropped, and kept until replayed, since the dead letter is the only record that a consuming context (Messaging, Moderation) still holds the account's data. Dead-lettering one logs at ERROR (event type, dead letter id, context and account id; nothing else about the account) and writes `ErasureDeadLettered` to the Moderation outbox in the same transaction; every sockd start logs a WARN with the count of dead-lettered erasure events per context until they are replayed. In the unordered Messaging and Moderation outboxes a failing row never blocks the rows behind it; in the ordered Identity outbox it holds the rows behind it until it succeeds or dead-letters.
- Ordering: the Identity outbox is consumed by one leader at a time (session advisory lock) in strict id order, which is commit order (above), so events for an account apply in the order they committed. The Messaging and Moderation outboxes have no ordering guarantee. Handlers are idempotent and do not depend on cross-account order.
- Replay: dead-lettered rows, erasure events included, are replayed by moving them back to the outbox (operator CLI `sockd events replay`).

## Published: Identity to Messaging

Versioned: changing these is a breaking change between contexts, even inside one binary. Fields are only added.

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `HumanRegistered` | Account | `RegisterHuman` | `account_id` UUID | Messaging: create `Participant(Human)` | Published | none: single aggregate (`Participant` created) |
| `PuppetCreated` | Account | `CreatePuppet`, whoever runs it: a session, a machine key (the `PuppetCreator` row is Identity's alone and not in the payload), or `ConnectorGrant.Consent` for a new connector puppet. Consent that adopts a kept puppet emits nothing: the participant already exists | `puppet_id` UUID, `owner_id` UUID | Messaging: create `Participant(Puppet(owner))` | Published | none: single aggregate |
| `DeviceAdded` | Device | `Issue` of a `session` device only. Never for a machine key (`IssueMachineKey`) or a puppet token, the connector device included: neither is an endpoint deliveries go to | `device_id` UUID, `account_id` UUID, `issued_at` Timestamp: `clock_timestamp()` read under the outbox lock, written to the device's `created_at` and to this row's `created_at` in the same transaction | Messaging: create `Endpoint` and `Backfill` it | Published | `Backfill` (Messaging): see the Identity watermark |
| `DeviceRemoved` | Device | `Revoke` of a `session` device only (sign-out, `RevokeDevice`, sign-in eviction, `Suspend`). Never for a machine key or a puppet token: no endpoint was created for them | `device_id` UUID, `cause` `DeviceRemovalCause` (0 reads as `revoked`) | Messaging: delete `Endpoint`; an endpoint removed for any cause but `suspended` first passes its device deliveries to the human's endpoints issued after each message, or to account deliveries when the human has no other endpoint ([email sign-in, pass 2](email-sign-in-pass-2.md)); otherwise its push registration and device deliveries go with it; delete the device's send receipts | Published | none: single aggregate, receipts are keyed by device |
| `AccountErased` | Account | `Erase` (human or puppet), including `ErasePuppet` from a machine key and `ConnectorGrant.Revoke` with cause `owner` (via `ErasePuppet`) or `unredeemed` (the sweep), each one event for the puppet. `Revoke(erasure)` runs inside the owner's `EraseAccount` and adds no event of its own: the human's one `AccountErased` covers its puppets | `account_id` UUID | Messaging: run `Erasure`. Moderation: delete reports filed by or about the account. Each context, Identity in the erasing transaction, also deletes its own dead-lettered outbox rows whose payload names the account, except erasure events and their alerts (`AccountErased`, `ErasureDeadLettered`), so a dead letter outlives neither its account nor its 30 days | Published | `Erasure` (spans Participant, Block, Conversation, Message); Moderation's reaction is a single-aggregate delete per report |
| `PuppetArchiveChanged` | Account | `ArchivePuppet` when the archive is written, `UnarchivePuppet` when it is deleted, by the owner or the creating machine key. Never on a no-op | `puppet_id` UUID, `owner_id` UUID, `archived` bool | Messaging: insert or delete the puppet's `PuppetArchive` row, read by `RecipientPolicy` for the active-puppet rule; and `pg_notify('puppet_archives', payload)`, each replica writing a `PuppetArchive` event to the open `Subscribe` streams of `owner_id`'s human devices, for which `ListPuppets` at stream open is the reconciliation | Published; the stream event is its client form | none: Messaging's own projection row and a relay |
| `ProEntitlementChanged` | Account | Any write that moves a human's `ProActiveUntil()`: `PassProGate` starting the trial, `ApplySubscription`, the webhook's billing customer deletion, `GrantPro` and `RevokeProGrant` | `account_id` UUID, `active_until` Timestamp (the epoch when no source exists) | Messaging: upsert the human's `ProEntitlement(activeUntil)` | Published | none: Messaging's own projection row |
| `AccountSuspensionChanged` | Account | `Suspend` when the suspension is written, `Reinstate` when it is deleted. Never on a no-op | `account_id` UUID, `suspended` bool | Messaging: insert or delete the participant's `Suspension` row | Published | none: Messaging's own projection row |

A trial or grant ending needs no event: `activeUntil` is a time, and `RecipientPolicy` compares it with now at post.

### The Identity watermark

A message posted between a device's issue and its `DeviceAdded` being applied has no delivery for that device: recipients are fixed at post from `messaging.endpoints`. Two rules close the gap without a synchronous write across contexts.

- Backfill. Creating an `Endpoint`, by the `DeviceAdded` projection or by caller self-ensure (which takes `issued_at` from the `Principal`'s device `created_at`, the same value), first locks the account's conversations in `Conversation.Post`'s lock order, then the account's endpoint lock exclusive, then reads the human's memberships again and inserts a delivery for every held message in those conversations with `sentAt >= issued_at`, minus what `RecipientPolicy` shields. Idempotent through the delivery key. `Post` takes every human member's endpoint lock shared after its conversation lock and before reading recipients, so lock order is conversation, then account, on both sides. A `Post` committed before the backfill's endpoint lock is backfilled, including one that created the conversation or a membership the conversation locks could not see; one after it reads the new endpoint.
- Watermark. Messaging keeps one row, the Identity watermark: the `created_at` of the last Identity outbox event it applied, advanced in the transaction that applies the event, never backwards. A message whose last delivery is acked is deleted only once the watermark has reached its `sentAt`; until then it is held with no deliveries, and advancing the watermark deletes the held messages it has passed. Any unapplied Identity event has a `created_at` above the watermark (commit order), so no device issued before a message can still be unprojected once the watermark passes it, and the backfill above has run for it. `sentAt` is `clock_timestamp()` at insert, the same database clock.
- Idle heartbeat. With the Identity outbox empty the watermark would stall, and acked messages would be held until the next Identity event. The leader therefore, when it finds the outbox empty, takes the `identity.outbox` lock, rechecks that it is empty and reads the next `identity.outbox_clock` value (the same rule, so it stays strictly increasing with the events), commits that, and then moves the watermark to it in a second transaction so Identity writers wait only for the clock: every committed event is applied, and any later event takes a later timestamp. At most once per idle wait (30 s) and at startup.
- Residual: the watermark is sound while the database clock does not step backwards. The clock rule keeps Identity's own timestamps strictly increasing, but `sentAt` is the plain database clock: if it steps back, a message posted just after a device was issued can get a `sentAt` below that device's `issued_at` and miss its backfill. That needs a backward step inside the event's own projection window; Postgres hosts slew rather than step, and the window is the outbox delay (milliseconds unless the outbox is stalled).
- Replay: a replayed Identity row is stamped under the lock like a new write (`identity.outbox_clock`), so its `created_at` is above the watermark.
- Residual: a dead-lettered `DeviceAdded` (after 20 attempts) is never applied; the next event moves the watermark past it and acked messages for that device are deleted. The device recovers through caller self-ensure, for messages that survived. While a head row backs off, every acked message is held, and retention grows until it is applied or dead-lettered. Events written before `issued_at` existed carry none and backfill nothing.
- Cost: during an Identity outbox stall, acked messages pile up in Messaging until it recovers, bounded by the stall, and by 30 days (`Message.Expire`).

## Apple grant revocations (a queue, not an event)

A grant to revoke is a row in `identity.apple_revocations`, written in the transaction that erases the account or replaces the grant at sign-in. It is not in the ordered Identity outbox, so an Apple outage cannot hold `DeviceRemoved` or `AccountErased` behind it. Replicas lease rows like the Messaging outbox (`FOR UPDATE SKIP LOCKED`, 60s lease) and call `POST https://appleid.apple.com/auth/revoke`, form-encoded: `client_id` (the row's client: the bundle id for `app`, the Services ID for `web`; Apple refuses any other), `client_secret` (ES256 JWT signed with the Sign in with Apple key: `iss` team id, `sub` the same client id, `aud` `https://appleid.apple.com`, `kid` key id, 5-minute expiry), `token`, `token_type_hint=refresh_token`. `200`, or `400 invalid_grant` (already revoked or expired), deletes the row. `5xx`, `429` or a network failure reschedules with backoff, honoring `Retry-After`. After 20 attempts the row is deleted and the failure logged with the account id only; the token is never logged, put in a URL or kept in a dead-letter table.

## Trial reminders and billing customer deletions (queues, not events)

Three more Identity queues, kept out of the ordered outbox for the same reason: a Cloudflare or Stripe outage must not hold an event behind it. `PassProGate` inserts the `TrialReminder` row when it starts a trial, `Erase` the `BillingCustomerDeletion` row for a human with a billing customer, and `Suspend` the `BillingCancellation` row for a human with a billing customer (`ApplySubscription` too, while the human is suspended), each notifying `trial_reminders`, `billing_customer_deletions` or `billing_cancellations` in the same transaction. Each worker leases rows as `identity.email_identity_deletions` does ([email sign-in, pass 2](email-sign-in-pass-2.md); `FOR UPDATE SKIP LOCKED`, sleeping until the earliest `available_at`), backs off on a timeout, `5xx` or throttle (1 s doubling to 1 h, or `Retry-After`) and handles each row as the domain model says: a reminder is sent or dropped and deleted; a deletion is deleted once Stripe answers `2xx` or `404` and otherwise retries at the 1 h ceiling with no cap, since a customer left behind keeps charging the card; a cancellation likewise, done once Stripe lists no open checkout and no live subscription for the customer.

## Published: Moderation to Identity

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `ReportResolved` | Report | `Resolve` | `report_id` UUID, `subject_id` UUID, `outcome` `ResolutionOutcome` | Identity: `subjectSuspended` suspends the subject; `ownerSuspended` suspends the subject's owner (the subject is a puppet), which takes all its puppets; each is a no-op if the account is gone or already suspended. `dismissed` needs no reaction | Published | none: single aggregate (`Account`) |

`Account.Suspend` emits `DeviceRemoved` for each session it revokes, so Messaging drops the endpoints through the existing path. The machine keys and puppet tokens it revokes, the connector device included, emit nothing.

## Internal: Identity, invite mail

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `InviteRequestApproved` | InviteRequest | `Approve`, only when it records the decision | `request_id` UUID | `InviteMailer`: mails the kept code once and records the delivery. Written to `identity.invite_mail_outbox`, leased, never `identity.outbox`. Full row in [invite-requests](invite-requests.md) | Internal | `InviteMailer` |

## Internal: Moderation

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `ReportFiled` | Report | `File` | `report_id` UUID | `ModerationAlert`: posts a short alert to the operator from the moderation puppet (`SOCK_MODERATION_PUPPET_ID`): report id, reporter and subject handles or puppet names, reason, evidence count. No note and no evidence text: those are read with `ListReports` (the console) or `sockd reports show`, so the alert always fits one message and carries nothing a reporter wrote. A report already erased completes without posting | Internal | `ModerationAlert` (spans Report, Account read, Conversation) |
| `ErasureDeadLettered` | none: the outbox, when an erasure event dead-letters | the dead-lettering transaction | `outbox` (the context whose `outbox_dead` holds it), `dead_letter_id` int64, `event_type`, `account_id` UUID, `attempts` | `ModerationAlert`: posts a short alert to the operator from the moderation puppet: the event type, account id, outbox, dead letter id and attempts, and `sockd events replay`. Idempotency key `erasure-dead-letter:<outbox>:<dead_letter_id>`, so one alert per dead letter; a replayed event that dead-letters again has a new id and alerts again. Held, unattempted, like `ReportFiled` while the moderation puppet is not configured. Its own dead letters are never swept either: one is the only record that the operator was never told | Internal | `ModerationAlert` (Conversation) |

`ReportResolved` is emitted the same way whether `ResolveReport` or `sockd reports resolve` resolved the report. A same-outcome retry of `ResolveReport` changes nothing and emits nothing.

The operator's actions add no events; the `OperatorAction` audit is written in each action's transaction, not through an outbox: a minted invite is read by `RegisterWithApple`, a reinstatement and an operator token revocation are read by the next request that authenticates, and nothing in another context holds a copy of any of them.

Puppet token issue and revoke emit nothing: puppets have no endpoints, and a revoked token's send receipts expire within 24 hours. A human's erasure emits one `AccountErased` for the human; Messaging's `Erasure` finds and removes the puppets itself, so it never sees a puppet whose owner is gone.

## Machine keys, connector grants and the support puppet (no events)

None of the three adds an event. For each, why none:

- Machine keys: `IssueMachineKey` emits no `DeviceAdded` and revoking a key no `DeviceRemoved`. Messaging's `Endpoint` is where deliveries go, and a key never receives one; an endpoint for it would hold deliveries no one acks. `PuppetCreator` is Identity's own fact: Messaging needs only the owner, which `PuppetCreated` carries.
- Connector grants: every OAuth transition (client registration, authorization requests, consent, code redemption, refresh and rotation, revocation records, the connector device, connector service tokens) is internal to Identity and nothing in another context holds a copy. `sock-mcpd` is not an event consumer: it learns of a revoked grant from `invalid_grant` at `/oauth/token` and of a deleted device from `TOKEN_INVALID` on its puppet calls, at once. Messaging sees a connector puppet only through `PuppetCreated` and `AccountErased`, as for any puppet; a kept puppet with no device simply has no consumer until a consent adopts it.
- The support puppet: configuration (`SOCK_SUPPORT_PUPPET_ID`), not a transition. `CreateDirect` with it emits nothing, as for any direct; its replies are `MessagePosted` like any message.

## Internal: Messaging

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `MessagePosted` | Message (issued by Conversation) | `Conversation.Post`, only when the message has ≥ 1 recipient | `message_id` UUID | (1) Stream wake: `pg_notify('deliveries', message_id)` in the same transaction; each replica loads the message's targets and writes to its live streams. (2) Push worker: outbox row, see below | Internal | none: reactions read one message and touch only `Endpoint` |

| `ReadMarkerAdvanced` | ReadMarker | `Advance`, only when `readThroughSeq` rose (a create counts) | `account_id` UUID, `conversation_id` UUID, `read_through_seq` int64 | Stream wake: `pg_notify('read_markers', payload)` in the same transaction; each replica writes a `ReadMarker` event to the open `Subscribe` streams of `account_id`, every device including the caller's. Not an outbox row: `ListConversations` at stream open is the reconciliation | Internal; the stream event is the published form, additive | none: a stream write, not a rule ([read markers](read-markers.md)) |

| `ActivityChanged` | none: `Activity` is an ephemeral value | `SetActivity`; `Conversation.Post` by an account `WORKING` there (as `IDLE`); `Leave` or `RemoveMember` that ends a `WORKING` account's membership, an owner's puppets leaving with it included (as `IDLE`) | `account_id` UUID, `conversation_id` UUID, `state`, `expires_at` (Unix ms, 0 for `IDLE`) | Stream wake: `pg_notify('activity', payload)`, outside any message transaction; each replica updates its in-memory board (replay, auto-IDLE) and writes an `Activity` event to the open `Subscribe` streams of the conversation's other members, shielded members excepted; a `WORKING` reaches no one once its setter is no longer a member. Not stored, not an outbox row: the 10 s refresh and the 30 s TTL reconcile | Internal; the stream event is the published form, additive | none: a stream write; who sees it is `RecipientPolicy` |
Stream wake is at-most-once by nature of `NOTIFY`. Correctness does not rest on it: a stream sends every pending delivery when it opens, and when a replica's `LISTEN` connection drops it re-sends pending deliveries to all its open streams once it reconnects.

Push worker. `MessagePosted` fans out: its handler writes one internal `PushDevice` outbox row (`message_id`, `device_id`, `apns_token`) per device delivery still pending whose endpoint has a push registration, in the same transaction that consumes the `MessagePosted` row. Each `PushDevice` row is processed and retried on its own, so one device's failure never re-alerts another. Per `PushDevice` row:
1. Wait until 2s after `sentAt` (a device with a live stream acks within that window).
2. For each device delivery still pending whose endpoint has a push registration, send an APNs alert, `mutable-content: 1`, `apns-collapse-id` = conversation id, payload `{conversation_id}` only. No message text.
3. APNs `410`, or `400` with reason `BadDeviceToken` or `DeviceTokenNotForTopic` → `Endpoint.Unregister()` for that token only (each means the token can never work). `429` or `5xx` → retry this row with backoff, honoring `Retry-After` if present. Any other `4xx` is permanent: log and drop.

Puppet deliveries are never pushed.

## Internal: History Transfer

| Name | Emitting aggregate | Emitting transition | Payload | Consumers | Boundary | Domain service |
|---|---|---|---|---|---|---|
| `HistoryTransferChanged` | HistoryTransfer | `Open` (row created), `Claim` (row created), `Append` (chunk or seal stored), `Close` (row deleted), the sweep (row deleted) | The binding tag, hex. Nothing else | `pg_notify('history_transfers', tag)` in the same transaction. Stream wake: each replica wakes its `ReceiveHistory` streams for that tag, which re-read the rows. Sweeper wake: each replica's sweeper re-reads its next deadline, so an unclaimed transfer is deleted at its 10 minutes | Internal; the stream events are the published form | none: the reaction is a read |

At most once (`NOTIFY`). Not an outbox row: the rows are the truth, and the heartbeat re-read bounds a lost notification to 30 s. `onReconnect` wakes every stream, as the delivery hub does. On `Open` no stream waits yet; the notify is for the sweeper.

## Deadlines (not events)

One sweeper per replica sleeps until the earliest deadline across these, capped at one hour, then deletes what has passed, under `FOR UPDATE SKIP LOCKED`:

| What | Deadline |
|---|---|
| Message and its deliveries | `sentAt` + the message hold (default 30 days) |
| Send receipt | `createdAt` + 24 hours |
| Invite | `expiresAt` |
| Invite request ([invite-requests](invite-requests.md)) | `decidedAt` + 30 days once decided; `createdAt` + 90 days while pending. Its decision, delivery and kept code go with it |
| Invite request bucket per client network | `refilledAt` + 1 hour |
| Operator token, its revocation and its sign-in fact | `expiresAt` + 30 days (kept so a token id in a log can be matched to its label); skipped while an `InviteMint` still names it |
| Operator action and its target | `at` + 365 days |
| Resolved report and its evidence | `resolvedAt` + 180 days |
| Sign-in bucket per client network | `refilledAt` + 10 minutes (a full bucket carries no information) |
| Report bucket per reporter | `refilledAt` + 24 hours |
| OAuth authorization request | `createdAt` + 10 minutes, with its verified identity; deleted at once when consent consumes it |
| Authorization code | `createdAt` + 1 hour (redeemable for the first 60 seconds; kept so a replay is seen), with its redemption |
| Connector grant whose code expired unredeemed | `createdAt` of its last code + 60 seconds with no redemption: not a delete but `ConnectorGrant.Revoke(unredeemed)`, which writes the revocation record and erases the new puppet (`AccountErased`). A puppet the consent adopted is not erased: the grant stays kept, its record cause `unredeemed`, and a later consent may adopt it again |
| History transfer and all it owns ([history transfer](history-transfer.md)) | `openedAt` + 10 minutes while unclaimed, `openedAt` + 60 minutes in any case; the sweep sends `HistoryTransferChanged` for each |
| Refresh token and its rotation | `createdAt` + 30 days; a grant with no live refresh token stays live until the owner erases its puppet, and its platform re-consents |
| Connector grant revocation record | `revokedAt` + 365 days |
| Dynamically registered OAuth client | `seen_at` + 30 days, only while no connector grant names it |
| Metadata document client (`cimd`) | refetched when its cached copy is older than its `Cache-Control` max-age, at most 24 hours; deleted like a `dcr` client once 30 days unused with no grant |
| Connector service token | `expiresAt`: refused from then, deleted 30 days later, as operator tokens are |
| Dead-lettered outbox rows (all contexts), except erasure events and their alerts | `dead_at` + 30 days, or when the account their payload names is erased, whichever is first; a dead-lettered `AccountErased` or `ErasureDeadLettered` has no deadline and stays until replayed |

## Cross-check

Every aggregate transition, traced through the three contracts. DDL table names refer to the server's data model; the two operator token tables, `identity.devices.kind`, `identity.puppet_creators` and the OAuth and connector tables are proposed DDL until the server migrates them. Machine keys, connector grants and the support puppet emit no events of their own (above).

| Transition | API | Event | DDL |
|---|---|---|---|
| `Account.RegisterHuman` | `RegisterWithApple` | `HumanRegistered` | `identity.accounts`, `identity.humans` |
| `TerminalSignIn.Start`, `Claim`, `Approve` | `StartTerminalSignIn`, `POST /login`, `GET /workos/terminal-callback` ([terminal sign-in](terminal-sign-in.md)) | none: private to the sign-in | `identity.terminal_sign_ins`, `identity.terminal_sign_in_claims`, `identity.terminal_sign_in_approvals` |
| `TerminalSignIn.Finish` | `FinishTerminalSignIn` | `DeviceAdded` when it issues a session; none for a ticket | deletes `identity.terminal_sign_ins` (cascading); `identity.devices` or `identity.registration_tickets` |
| `Account.CreatePuppet` | `CreatePuppet` (session or machine key), `POST /oauth/consent` (Allow, new puppet) | `PuppetCreated` | `identity.accounts`, `identity.puppets`; no puppet count |
| `Account.UpdateDisplayName` | `UpdateDisplayName` | none: Messaging holds no names | `identity.accounts.display_name` |
| `Account.ChangeHandle` | `ChangeHandle` | none: Messaging holds no handles | `identity.humans.handle` |
| `Account.Erase` | `EraseAccount`, `ErasePuppet` (session or machine key), `EraseHuman` (operator, a human only) | `AccountErased`; queues the grant in `identity.apple_revocations` and a human's billing customer in `identity.billing_customer_deletions` | cascades from `identity.accounts` (devices, machine keys, `identity.puppet_creators`, `identity.connector_grants` and what they own); deletes `identity.operator_actions` whose target names the account or its puppets |
| `Account.AcceptTerms` | `RegisterWithApple`, `AcceptTerms` | none: read into the `Principal` per request | `identity.terms_acceptances` |
| `Account.RecordAppleGrant` | `SignInWithApple`, `RegisterWithApple`, either Apple client | none; queues the replaced grant, with its client, in `identity.apple_revocations` | `identity.apple_grants` (with `client`), `identity.apple_revocations` (with `client`), `identity.apple_clients`; columns proposed |
| Message evidence tag | `Send`, `NotifyOwner` (returned), `Subscribe` / `FetchPending` (delivered) | none: computed from the message's fields and the server key, never stored | none |
| `Account.Suspend` | none directly: operator CLI, or `ReportResolved` (from `ResolveReport` or `sockd reports resolve`). No `SuspendAccount` RPC: over the network an account is suspended only by upholding a report | consumes `ReportResolved`; emits `AccountSuspensionChanged` and `DeviceRemoved` per session; machine keys and puppet tokens, the connector device included, go with no event; queues a human's billing customer in `identity.billing_cancellations` | `identity.suspensions`, `identity.devices` delete (cascading to `identity.puppet_creators` and `identity.connector_puppet_devices`), `identity.billing_cancellations` insert |
| `Account.Reinstate` | `ReinstateAccount` (and `sockd accounts reinstate`) | `AccountSuspensionChanged` (not on a no-op); devices are not restored, the human signs in again | `identity.suspensions` delete |
| `Account.CreateEmailHuman` (network) | `CreateHuman` ([operator human lifecycle](operator-human-lifecycle.md)) | `HumanRegistered` | `identity.accounts`, `identity.humans`, `identity.email_identities`, `identity.operator_actions`, `identity.operator_action_human_lifecycles` |
| `Account.ArchiveHuman` | `ArchiveHuman` | `AccountSuspensionChanged` when the effective suspension changes, `DeviceRemoved` per session | `identity.human_archives` insert, `identity.devices` delete, `identity.billing_cancellations`, `identity.operator_actions` |
| `Account.UnarchiveHuman` | `UnarchiveHuman` | `AccountSuspensionChanged` when the effective suspension changes | `identity.human_archives` delete, `identity.operator_actions` |
| `Participant.Block` / `Unblock` | `BlockAccount`, `UnblockAccount` | none: read at post time by `RecipientPolicy` | `messaging.blocks` |
| `Report.File` | `FileReport` | `ReportFiled` | `moderation.reports`, `moderation.reported_messages`, `moderation.report_buckets` |
| `Report.Resolve` | `ResolveReport` (and `sockd reports resolve`) | `ReportResolved`, none for a same-outcome retry | `moderation.resolutions` |
| Moderation erasure | none: event-driven | consumes `AccountErased` | `moderation.reports` delete (cascading) |
| Report expiry | none: deadline | none | `moderation.reports` delete |
| `Device.Issue` | `SignInWithApple`, `RegisterWithApple` (app or CLI), `CreatePuppet`, `MintPuppetToken` (session or machine key) | `DeviceAdded` with `issued_at` for a `session` (CLI sessions included); none for a puppet token. The device's `created_at` is set to that timestamp under the outbox lock. A human already at 10 live sessions has the oldest-issued session removed in the same transaction first, which emits its `DeviceRemoved` (freeing its push registration); machine keys are neither counted nor evicted; sign-in is never refused for the limit | `identity.devices` (with `kind`) |
| `Device.Revoke` | `SignOut` (app, `sock logout`), `RevokeDevice` | `DeviceRemoved` (sessions only) | `identity.devices`; a machine key's revocation cascades to `identity.puppet_creators`, a connector device's to `identity.connector_puppet_devices` |
| `Device.IssueMachineKey` | `MintMachineKey` | none: a machine key is not an endpoint | `identity.devices` (`kind = 'machineKey'`), counted under the human's limits lock |
| `Device.Verify` (prefix and kind agree) | every authenticated RPC | none: read | `identity.devices.kind`, `identity.device_kinds` |
| `PuppetCreator` recorded | `CreatePuppet` from a machine key | none: covered by `PuppetCreated`, whose payload needs no creator | `identity.puppet_creators` |
| `Account.ArchivePuppet`, `Account.UnarchivePuppet` | `ArchivePuppet`, `UnarchivePuppet` | `PuppetArchiveChanged` (not on a no-op) | `identity.puppet_archives` |
| `Account.PassProGate` (starts the trial) | `CreatePuppet`, `MintPuppetToken`, `MintMachineKey`, `IssueConnectorPuppetToken`, `POST /oauth/consent` (Allow) | `ProEntitlementChanged`; queues the `TrialReminder` | `identity.pro_trials`, `identity.trial_reminders` |
| `TrialReminder` handled | none: queue worker | none | `identity.trial_reminders` delete |
| `Account.BindBillingCustomer` | `StartCheckout` (first time) | none: Identity's own row, nothing else reacts | `identity.billing_customers` |
| `Account.ApplySubscription` | `POST /stripe/webhook` | `ProEntitlementChanged` when it moves `ProActiveUntil()` | `identity.pro_subscriptions` upsert |
| Billing customer deleted by Stripe | `POST /stripe/webhook` (`customer.deleted`) | `ProEntitlementChanged` when it moves `ProActiveUntil()` | `identity.billing_customers` delete, cascading to `identity.pro_subscriptions` |
| `BillingCustomerDeletion` handled | none: queue worker | none | `identity.billing_customer_deletions` delete |
| `BillingCancellation` handled | none: queue worker | none: Stripe's `customer.subscription.deleted` webhook then applies the end | `identity.billing_cancellations` delete |
| `Account.GrantPro` | `GrantPro` | `ProEntitlementChanged` when it moves `ProActiveUntil()` | `identity.pro_grants`, with `identity.operator_actions` and `identity.operator_action_pro_grants` |
| `Account.RevokeProGrant` | `RevokeProGrant` | `ProEntitlementChanged` when it moves `ProActiveUntil()` | `identity.pro_grant_revocations`, with the operator action rows |
| Sock Pro read | `GetMe`, `GetHuman` | none: derived from the sources at read | reads `identity.pro_trials`, `identity.pro_subscriptions`, `identity.pro_grants`, `identity.pro_grant_revocations` |
| `PuppetCreator` deleted | `RevokeDevice` (the key), `ErasePuppet`, `EraseAccount`, `Suspend` (the key goes) | none: Identity's own fact; the puppet stays the owner's | `identity.puppet_creators` delete, by cascade |
| `OAuthClient.Register` | `POST /oauth/register` | none: nothing outside Identity holds clients | `identity.oauth_clients` (`kind = 'dcr'`), `identity.oauth_client_redirect_uris` |
| `OAuthClient` metadata fetch | `GET /oauth/authorize` with an `https` `client_id` | none | `identity.oauth_clients` (`kind = 'cimd'`, `seen_at`), `identity.oauth_client_redirect_uris` replaced |
| `OAuthClient` sweep | none: deadline, 30 days unused and no grant | none | `identity.oauth_clients` delete (RESTRICT from `identity.connector_grants` keeps a client with a grant) |
| `AuthorizationRequest` open | `GET /oauth/authorize` | none: short-lived, Identity only | `identity.authorization_requests` |
| `AuthorizationRequest.Verify` | `POST /apple/oauth-callback` | none | `identity.authorization_request_identities` |
| `AuthorizationRequest` expiry | none: deadline, 10 minutes | none | `identity.authorization_requests` delete, cascading to its identity |
| `ConnectorGrant.Consent` | `POST /oauth/consent` (Allow) | `PuppetCreated` for a new puppet (through `Account.CreatePuppet`); none when it adopts a kept puppet | `identity.connector_grants` (an adopted kept grant's row deleted first), `identity.authorization_codes`, `identity.accounts` and `identity.puppets` for a new puppet, `identity.authorization_requests` delete |
| Consent denied | `POST /oauth/consent` (Deny) | none: no grant exists | `identity.authorization_requests` delete |
| `ConnectorGrant.Redeem` | `POST /oauth/token` (`authorization_code`) | none: tokens are Identity's | `identity.authorization_code_redemptions`, `identity.refresh_tokens` |
| `ConnectorGrant.Refresh` | `POST /oauth/token` (`refresh_token`) | none | `identity.refresh_tokens`, `identity.refresh_token_rotations`; a retry keeps the unused successor as a tombstone in `identity.refresh_token_voids` (not deleted), within 60 s of the first rotation |
| `ConnectorGrant.IssueAccessToken` | `POST /oauth/token` (both grant types) | none | none: the JWT is not stored; the key is configuration |
| `ConnectorGrant.IssueConnectorDevice` | `IssueConnectorPuppetToken` | none: a puppet token is not an endpoint | `identity.devices` (`kind = 'puppetToken'`, label `connector`), `identity.connector_puppet_devices` |
| `ConnectorGrant.RevokeConnectorDevice` | `RevokeConnectorDevice` | none | `identity.devices` delete, cascading to `identity.connector_puppet_devices` |
| `ConnectorGrant.Revoke(client)` | `POST /oauth/revoke` | none: the puppet is kept | `identity.connector_grant_revocations`; `identity.authorization_codes`, `identity.refresh_tokens` and the connector device deleted |
| `ConnectorGrant.Revoke(refreshReuse)` | `POST /oauth/token` (a rotated token whose successor was used) | none: the puppet is kept | same as `client` |
| `ConnectorGrant.Revoke(codeReplay)` | `POST /oauth/token` (a redeemed code presented again) | none: the puppet is kept | same as `client` |
| `ConnectorGrant.Revoke(owner)` | `ErasePuppet` on a connector puppet | `AccountErased` for the puppet | `identity.connector_grant_revocations`, then the puppet's `identity.accounts` delete cascading to the grant |
| `ConnectorGrant.Revoke(erasure)` | `EraseAccount` | `AccountErased`, the human's one | `identity.connector_grant_revocations` per live grant, then the cascade from the human |
| `ConnectorGrant.Revoke(unredeemed)` | none: deadline, the code expired unredeemed | `AccountErased` for a new puppet; none for an adopted one, which stays kept | `identity.connector_grant_revocations`, the puppet's `identity.accounts` delete |
| Connector grant revocation expiry | none: deadline, 365 days | none | `identity.connector_grant_revocations` delete |
| `ConnectorServiceToken.Mint` | none: bootstrap CLI only (`sockd connector token mint`), so a leaked token cannot mint another | none | `identity.connector_service_tokens` |
| `ConnectorServiceToken.Verify` | `IssueConnectorPuppetToken`, `RevokeConnectorDevice` | none: read | `identity.connector_service_tokens` |
| `ConnectorServiceToken` revoke and expiry | none: `sockd connector token revoke` (deletion), deadline | none: read by the next connector call | `identity.connector_service_tokens` delete |
| Support puppet | `GetMe` (`support_account_id`), `CreateDirect` and `MintPuppetToken` admit it | none: configuration, not a transition; `Conversation.StartDirect` and `Device.Issue` widen their guards | none: `SOCK_SUPPORT_PUPPET_ID` is configuration |
| `Invite.Mint` | `MintInvite` (and `sockd invite mint`) | none: nothing reacts | `identity.invites` |
| `OperatorToken.Mint` | none: bootstrap CLI only (`sockd operator token mint`), so a leaked token cannot mint another | none: nothing reacts | `identity.operator_tokens` |
| `OperatorToken.SignIn` | `SignInOperatorWithApple` (anonymous, Apple identity of `SOCK_OPERATOR_APPLE_SUB`) | none: the token is read by the next `OperatorService` call | `identity.operator_tokens`, `identity.operator_token_sign_ins`, with `identity.operator_actions` (`signInWithApple`) in the same transaction |
| `OperatorToken.Revoke` | `RevokeOperatorToken` (the presenting token), `sockd operator token revoke` (any) | none: read by the next operator request; the token's invites are deleted in the same transaction, so nothing needs to react | `identity.operator_token_revocations`, `identity.invites` delete via `identity.invite_mints` unless an `identity.operator_token_sign_ins` row exists |
| `InviteMint` recorded | `MintInvite` (not `sockd invite mint`) | none: read only by `OperatorToken.Revoke` | `identity.invite_mints` |
| `OperatorAction.Record` | `MintInvite`, `ResolveReport`, `ReinstateAccount`, `RevokeOperatorToken`, `SignInOperatorWithApple`, `EraseHuman`, `SetMessageHold`, `GrantPro`, `RevokeProGrant`, in each call's transaction; `ApproveInviteRequests`, `DeclineInviteRequests`, in each changed request's transaction | none: an audit, read by people | `identity.operator_actions`, `identity.operator_action_resolutions`, `identity.operator_action_reinstatements`, `identity.operator_action_invite_requests`, `identity.operator_action_pro_grants` |
| Operator action expiry | none: deadline | none | `identity.operator_actions` delete, cascading to its target row |
| `OperatorToken.Verify` | every `OperatorService` RPC | none: read | `identity.operator_tokens`, `identity.operator_token_revocations` (absence) |
| Apple web callback relay | `GET /apple/cli-login`, `POST /apple/cli-callback`, `POST /apple/console-callback` (HTTP routes, not RPCs) | none: no state changes | none: stateless |
| Operator token expiry | none: time passing; the sweeper deletes the row 30 days later | none | `identity.operator_tokens` delete, cascading to its revocation |
| `Invite.Redeem` | `RegisterWithApple` | none: covered by `HumanRegistered` | `identity.invites` delete |
| Invite expiry | none: deadline | none | `identity.invites` delete |
| `Participant` create | none: event-driven, plus caller self-ensure | consumes `HumanRegistered`, `PuppetCreated` | `messaging.participants`, `messaging.humans`, `messaging.puppets` |
| Projection of Pro, suspension and archive | none: event-driven | consumes `ProEntitlementChanged`, `AccountSuspensionChanged`, `PuppetArchiveChanged` | `messaging.pro_entitlements`, `messaging.suspensions`, `messaging.puppet_archives` |
| `Erasure` | none: event-driven | consumes `AccountErased` | cascades from `messaging.participants` |
| Erasure dead-lettered | none: event-driven | emits `ErasureDeadLettered` when `AccountErased` dead-letters | `identity.outbox_dead` (kept, never swept), `moderation.outbox` |
| `Endpoint` create / delete | none: event-driven, plus caller self-ensure | consumes `DeviceAdded`, `DeviceRemoved`; create runs `Backfill` | `messaging.endpoints`, `messaging.device_deliveries` (backfill) |
| `IdentityWatermark.Advance` | none: event-driven (every Identity event applied) and the idle heartbeat | consumes every Identity event; deletes the held messages it passes | `messaging.identity_watermark`, `messaging.messages` delete |
| `Endpoint.RegisterPush` | `RegisterPush` | none: nothing reacts | `messaging.push_registrations` |
| `Endpoint.Unregister` | none: APNs 410 | none | `messaging.push_registrations` delete |
| `Conversation.StartDirect` | `CreateDirect`, `NotifyOwner` | none: recipients learn of it from its first message | `messaging.conversations`, `messaging.memberships`, `messaging.direct_pairs`. Never two humans |
| `Conversation.StartGroup` | `CreateGroup` | none: same | `messaging.conversations`, `messaging.memberships`; reads the three projections (an active puppet) |
| `Conversation.AddMember` / `RemoveMember` / `Leave` | same names | none: members see changes via `ListConversations`. Open: membership notices | `messaging.memberships` |
| `ReadMarker.Advance` | `MarkRead` (and `ListConversations`, `ExportMessagingData` read it) | `ReadMarkerAdvanced` | `messaging.read_markers` |
| `Conversation.Post` | `Send`, `NotifyOwner` | `MessagePosted` | `messaging.conversations.next_seq`, `messaging.messages`, `messaging.*_deliveries`, `messaging.send_receipts`; a human sender reads the three projections (an active puppet) |
| Read-only conversation | `ListConversations` (`humans_can_post`) | none: derived at read | reads the three projections |
| `Message.Ack` | `Ack` | none: nothing reacts | `messaging.*_deliveries` delete, `messaging.messages` delete on last delivery once the watermark has reached `sentAt`, else the message stays held with none |
| `HistoryTransfer.Open` | `OpenHistoryTransfer` | `HistoryTransferChanged`: wakes the sweeper for the claim deadline | `transfer.transfers` |
| `HistoryTransfer.Admits` | every `HistoryTransferService` RPC | none: a read | `transfer.transfers` by primary key |
| `HistoryTransfer.Claim` | `ClaimHistoryTransfer` | `HistoryTransferChanged` | `transfer.claims` |
| `HistoryTransfer.Append` | `UploadHistoryChunk` | `HistoryTransferChanged` | `transfer.chunks`; with `final`, `transfer.seals` |
| `HistoryTransfer.Close` / expiry | `CloseHistoryTransfer`; none: the sweeper | `HistoryTransferChanged` | delete `transfer.transfers`, cascade |
| `Message.Expire` | none: deadline | none | `messaging.messages` delete |

## Open

- Whether Messaging should stop puppet deliveries accumulating for a suspended puppet. It holds the suspension (`AccountSuspensionChanged`), but only the active-puppet rule reads it; v1: the puppet's tokens are revoked, and deliveries expire with the message hold.
- Membership change notices to other members (system messages such as "X added Y"). v1: none, clients refresh `ListConversations` when a message arrives for an unknown conversation.
