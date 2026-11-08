# Operator human lifecycle: create and archive

Pass 1, draft for approval. Context: Identity. Builds on [email sign-in](email-sign-in.md) (`CreateEmailHuman`, `sockd humans create`), [email sign-in, pass 2](email-sign-in-pass-2.md) (`email_identity_deletions`), [feature flags](feature-flags.md) (open registration) and the operator RPCs in [api](api.md).

What changes:

- The operator creates a human from the console: a network `CreateHuman`, the email path of `sockd humans create`. The person need not be a test account; nothing marks one.
- The operator archives a human instead of erasing it at once. An archived human is out of the default Users list, cannot sign in and holds no devices. Unarchive restores the list entry and sign-in. `EraseHuman` is unchanged and stays the irreversible step.
- The console gets Create, Archive, Unarchive and an Archived filter on the Users page; Erase moves behind Archive there.

Invites play no part: registration is open, and the operator's creation is its own admission.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Created human | A human made by the operator, with an email identity and a generated password. Indistinguishable from a registered one afterwards | Not stored: the human is the trace; `OperatorAction(createHuman)` records who made it, for 365 days |
| Archive | The operator's fact that a human is shelved: signed out everywhere, refused sign-in, listed only under the Archived filter | Stored (`identity.human_archives`) |
| Effectively suspended | A suspension row exists or an archive row exists. What the Principal check, sign-in and Messaging act on | Derived |
| Initial password | The 24 random characters set at WorkOS when the operator creates a human. Returned once, never stored by sock | Not stored |

Terms not changed: Suspend, Reinstate, Erase mean what [domain-model](domain-model.md) says. Archive here is the human's; a puppet's archive (`PuppetArchive`) is the owner's own shelving of an agent and shares nothing with it but the word. Inferred, to confirm: the two stay separate objects.

## Domain model

Changes to [domain-model](domain-model.md). Unchanged objects are not repeated.

### Identity

#### Account (changed)

New behaviors on the root, humans only:

- `CreateEmailHuman(emailIdentity, handle, displayName)`: unchanged, now also reached by `CreateHuman`.
- `ArchiveHuman()`: writes a `HumanArchive` if absent, revokes every device of the human and of its puppets as `Suspend` does, and queues a `BillingCancellation` if the human has a `BillingCustomer` and none is pending, as `Suspend` does. Refuses a human owning a system puppet (`SYSTEM_PUPPET`). A no-op when archived. Emits `AccountSuspensionChanged` when the effective suspension goes from false to true.
- `UnarchiveHuman()`: deletes the `HumanArchive` if present. Devices are not restored; the human signs in again. A no-op when not archived. Emits `AccountSuspensionChanged` when the effective suspension goes from true to false (a standing report suspension keeps it true, so nothing is emitted then).
- `Suspend()` and `Reinstate()`: unchanged, except that their `AccountSuspensionChanged` follows the effective value, not the row.
- `Erase()`: unchanged. An archived human is erasable; its `HumanArchive` goes with it.

Invariants:

- An archived human has no live device and signs in to nothing: sign-in, refresh and the Principal check refuse it as they refuse a suspended one.
- Unarchive never reinstates: a suspension and an archive are independent facts and each is removed only by its own operation.
- A human is archived at most once.

#### HumanArchive

Value object, owned by `Account` (humans), 0 or 1. The fact that the operator archived this human.

| Field | Type | Meaning |
|---|---|---|
| `accountId` | UUID | The human. Identity of the row |
| `archivedAt` | Timestamp | Server clock when the archive was written |

Relationships: `Account` (human) has-a `HumanArchive`, 1 to 0..1. No row: not archived. Which operator token archived it, and when it was unarchived, is the `OperatorAction` audit, not this row.

State machine, per human:

| From | To | Trigger |
|---|---|---|
| live | archived | `ArchiveHuman` |
| archived | live | `UnarchiveHuman` |
| live, archived | erased | `Erase` |

Anything unlisted is refused by the aggregate, except the two no-ops above, which succeed unchanged.

#### OperatorAction (changed)

Kinds gain `createHuman`, `archiveHuman` and `unarchiveHuman`. Each targets one account: the created human, or the archived or unarchived one. Same 365-day sweep as the other kinds; target table in the data layer below.

## API

New and changed rows of [api](api.md), `OperatorService`. Caller class: operator only, as every row here. No new caller class. Authentication: operator token as today.

| RPC | Caller | Authorization | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|
| `CreateHuman` | Operator | Any valid `full` operator token; the operator create bucket (Rate limits below) | `email`, `handle` (`Handle` rules, 3 to 20 of `[a-z0-9_]`, lowercased), `display_name` (1 to 64) | `account` (the new human), `initial_password` (shown once; sock stores nothing of it) | `FIELD_INVALID` (email rejected by WorkOS, handle or display name outside its rule), `HANDLE_TAKEN` (nothing created at WorkOS), `EMAIL_ALREADY_ADDED` (WorkOS has the address: nothing created in sock), `RATE_LIMITED`, `unavailable` (WorkOS unreachable: nothing created) | Not idempotent. A retry after a lost response fails `HANDLE_TAKEN` or `EMAIL_ALREADY_ADDED`, and the first call's password is gone; the operator archives and erases that human, or resets the password at WorkOS | `Account.CreateEmailHuman`, with `OperatorAction(createHuman, account)` in the same transaction. Emits `HumanRegistered`. CLI: `sockd humans create` (unchanged) |
| `ArchiveHuman` | Operator | Any valid `full` operator token. A human owning no system puppet | `account_id` | none | `FIELD_INVALID` (malformed id), `NOT_FOUND` (no such account or a puppet), `SYSTEM_PUPPET` | Idempotent: an archived human succeeds unchanged and records no action | `Account.ArchiveHuman` with `OperatorAction(archiveHuman, account)` in the same transaction (only when it wrote the archive). Emits `AccountSuspensionChanged` when the effective value changes, `DeviceRemoved` per session |
| `UnarchiveHuman` | Operator | Any valid `full` operator token. A human | `account_id` | none | `FIELD_INVALID`, `NOT_FOUND` | Idempotent: a human not archived succeeds unchanged and records no action | `Account.UnarchiveHuman` with `OperatorAction(unarchiveHuman, account)` in the same transaction (only when it deleted the archive) |
| `ListHumans` (changed) | Operator | As today | As today, plus `archive_filter` (unspecified or `LIVE` default, `ARCHIVED`, `ALL`) | As today; each `HumanSummary` gains `archived_at` (unset: not archived) | As today | A read | none (read) |
| `GetHuman` (changed) | Operator | As today | As today | `human` carries `archived_at` as above | As today | A read | none (read) |
| `EraseHuman` | Operator | Unchanged | Unchanged | Unchanged | Unchanged | Unchanged | Unchanged. An archived human is erasable; so is a live one, unless the open question below is decided otherwise |

The operator RPC count in [api](api.md) rises by three and the terms exempt list gains all three.

Auth, sign-in and refresh: a human with a `HumanArchive` is refused as a suspended human is, `ACCOUNT_SUSPENDED`. Inferred, to confirm: no new `ErrorReason`, because every client already handles `ACCOUNT_SUSPENDED` and an archived person has nothing different to be told.

Rate limits: `CreateHuman` takes from an operator create bucket of its own, 10 per hour per operator token, because each call creates a credential at a third party. `ArchiveHuman` and `UnarchiveHuman` draw from no bucket: no operator write bucket exists, and `GrantPro` and `SetFeatureFlag` draw none either. The create bucket is the `operatorCreate` kind in `oauth_buckets`, burst 10, drawn after validation and before WorkOS, never refunded; its TTL is 1 hour so a sweep cannot reset a drained bucket.

Proto, `operator.proto` (additive):

```proto
rpc CreateHuman(CreateHumanRequest) returns (CreateHumanResponse) {}
rpc ArchiveHuman(ArchiveHumanRequest) returns (ArchiveHumanResponse) {}
rpc UnarchiveHuman(UnarchiveHumanRequest) returns (UnarchiveHumanResponse) {}

message CreateHumanRequest {
  string email = 1;
  string handle = 2;
  string display_name = 3;
}
message CreateHumanResponse {
  Account account = 1;
  // 24 random characters, returned once. Never logged, never stored.
  string initial_password = 2;
}
message ArchiveHumanRequest { string account_id = 1; }
message ArchiveHumanResponse {}
message UnarchiveHumanRequest { string account_id = 1; }
message UnarchiveHumanResponse {}

enum HumanArchiveFilter {
  HUMAN_ARCHIVE_FILTER_UNSPECIFIED = 0;  // same as LIVE
  HUMAN_ARCHIVE_FILTER_LIVE = 1;
  HUMAN_ARCHIVE_FILTER_ARCHIVED = 2;
  HUMAN_ARCHIVE_FILTER_ALL = 3;
}
// ListHumansRequest gains: HumanArchiveFilter archive_filter = 4;
// HumanSummary gains: google.protobuf.Timestamp archived_at = 5;
```

## Domain events

| Event | Emitting aggregate | Transition | Payload | Consumers | Delivery | Boundary | Domain service |
|---|---|---|---|---|---|---|---|
| `HumanRegistered` (one more emitter) | Account | `CreateEmailHuman` via `CreateHuman` | `account_id` UUID, unchanged | Messaging: create `Participant(Human)` | As today: outbox, at least once | Published | none: single aggregate |
| `AccountSuspensionChanged` (changed meaning) | Account | The effective suspension changes: `Suspend` or `ArchiveHuman` takes it false to true, `Reinstate` or `UnarchiveHuman` takes it true to false. Never on a no-op or when the other fact still holds it | `account_id` UUID, `suspended` bool (the effective value), unchanged shape | Messaging: insert or delete the participant's `Suspension` row | As today | Published | none: Messaging's own projection row |
| `DeviceRemoved` (existing) | Account | One per session revoked by `ArchiveHuman` | unchanged | unchanged | unchanged | unchanged | none |

No new event: Messaging needs only the effective value, which the existing event already carries. Inferred, to confirm. `HumanArchive` needs no Messaging projection of its own.

## Data layer

Migration `00035_human_archives.sql` (number follows `00034`; adjust on merge).

```sql
CREATE TABLE identity.human_archives (
    account_id  uuid        PRIMARY KEY REFERENCES identity.humans (account_id) ON DELETE CASCADE,
        -- the row is the state: archived while it exists. Serves the Principal
        -- check, sign-in and ListHumans' archive filter
    archived_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX human_archives_archived_at_idx ON identity.human_archives (archived_at DESC);
    -- serves ListHumans with the ARCHIVED filter

INSERT INTO identity.operator_action_kinds (name)
VALUES ('createHuman'), ('archiveHuman'), ('unarchiveHuman');

CREATE TABLE identity.operator_action_human_lifecycles (
    action_id  bigint PRIMARY KEY REFERENCES identity.operator_actions (id) ON DELETE CASCADE,
    account_id uuid   NOT NULL
        -- the created, archived or unarchived human. No FK, as
        -- operator_action_reinstatements: an FK cascade would leave an action
        -- with no target; Account.Erase deletes the action itself
);
CREATE INDEX operator_action_human_lifecycles_account_idx ON identity.operator_action_human_lifecycles (account_id);
    -- serves Account.Erase (actions targeting the erased account)
```

`operator_action_reinstatements` stays `reinstateAccount`'s: its name and its one kind would make the three new kinds read wrongly. `identity.assert_action_target` gains the new table's count, matched to the three kinds, with the same deferred constraint trigger. `Account.Erase` deletes these actions as it deletes the others that target the erased account.

`ListHumans` with `LIVE` is `WHERE NOT EXISTS (SELECT 1 FROM identity.human_archives a WHERE a.account_id = h.account_id)`, `ARCHIVED` the inverse join, both on the primary key.

5NF: one row is one fact about one human, its time; nothing is derivable from it and no column depends on another. No nullable column. No JSON.

`CreateHuman` writes no new table: `accounts`, `humans`, `email_identities`, `terms_acceptances` (none: terms are accepted at first consent, as `CreateEmailHuman` records none). When the sock write fails after WorkOS created the user, the hub inserts the WorkOS user id into `identity.email_identity_deletions` in a second, separate transaction and notifies its worker, which deletes the user at WorkOS with the existing backoff. If that insert also fails the hub logs the WorkOS user id for the operator, as `sockd humans create` prints it.

## Console

Changes to the Users page. All writes are behind `full` scope as today.

- Create human: a form (email, handle, display name) above the list. On success it shows the handle and the initial password once, with a Copy button and "Shown once. sock does not keep it." The password lives in one DOM text node, never in `sessionStorage`, the URL or an `aria-live` region, and is cleared when the operator dismisses it, searches, opens another human or leaves the tab. The list reloads and opens the new human.
- Archived filter: Live (default), Archived, All, beside the handle search.
- Human detail: Archive (confirm naming the handle: "Archive @h? They are signed out everywhere and cannot sign in until unarchived.") on a live human; Unarchive on an archived one; Erase only on an archived human, behind a prompt that names it irreversible and asks for the handle, which is sent as `confirm_handle`.
- A hub that does not serve these yet answers `Unimplemented`, shown as "This hub does not serve that yet." Hiding the controls outright waits on `sock-console-7hl`.

## Threats

| Threat | Mitigation |
|---|---|
| A leaked `full` operator token creates accounts with known passwords | Every call is audited with the token id; 10 per hour per token; the token already reaches `EraseHuman` and `GrantPro`, so the added capability is credential creation at WorkOS, not a new privilege class. Accepted, pending security review |
| The initial password leaking in transit or logs | Returned only in this response; operator request and response bodies are never logged ([api](api.md)); the console holds it in one text node and clears it. It is a one-time credential the person should change |
| Partial failure leaving a WorkOS user with no sock human | Compensating deletion queued in `email_identity_deletions`, as erasure does |
| Creating a human that takes a handle someone wants | A created human is a real handle; Archive does not free the handle, Erase does. Accepted |
| An archived human regaining access through a still-valid token | Archive revokes every device in its transaction; sign-in and refresh refuse it |
| Archive hiding a human from moderation | `GetHuman` and `ListHumans` with `ALL` still read it; reports about it still list |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `CreateEmailHuman` (network) | `CreateHuman` | `HumanRegistered` | `accounts`, `humans`, `email_identities`, `operator_actions` and `operator_action_human_lifecycles` insert; `email_identity_deletions` on failure |
| `ArchiveHuman` | `ArchiveHuman` | `AccountSuspensionChanged` on effective change, `DeviceRemoved` per session | `human_archives` insert, `devices` delete, `billing_cancellations` insert, `operator_actions` insert |
| `UnarchiveHuman` | `UnarchiveHuman` | `AccountSuspensionChanged` on effective change | `human_archives` delete, `operator_actions` insert |
| `Erase` of an archived human | `EraseHuman`, unchanged | `AccountErased`, unchanged | cascade removes `human_archives` |
| Archive refused for a system puppet's owner | `ArchiveHuman` `SYSTEM_PUPPET` | none | none: a rule on the aggregate, as `EraseHuman`'s |
| Sign-in and Principal refusal for archived | all authenticated RPCs, sign-in RPCs | none | `human_archives` read |

## Open questions

- Should `EraseHuman` refuse a human that is not archived, so the hub, not only the console, enforces archive-first? It is a behavior change for any other caller of `EraseHuman` and needs a new `ErrorReason` (`NOT_ARCHIVED`). Left as console-only until decided.
- Is a created human's first sign-in by password enough, or does `CreateHuman` need to set `accept_terms`? Currently none: the person accepts at first consent, as `sockd humans create` does. A console-created account used for testing may want terms pre-accepted.
- Should an archived human's handle stay reserved? Currently yes: the `humans` row stays.
- Does a console-created human need Sock Pro to be testable? `GrantPro` already exists on the Users page; `CreateHuman` grants nothing.
- Is 10 creates per hour per token the right bucket?
