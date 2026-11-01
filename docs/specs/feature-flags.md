# Feature flags and open registration

Approved, pass 1. Contexts: Identity. Builds on [invite requests](invite-requests.md) (admission and the operator's audit), [email sign-in, pass 3](email-sign-in-pass-3.md) (the registration ticket) and the message hold ([email sign-in, pass 2](email-sign-in-pass-2.md)), whose shape every flag copies: a Postgres row the operator sets over the network, audited in the same transaction, with a `NOTIFY` that wakes whatever reads it.

What changes:

- The hub gains platform flags: a closed set of keys the hub declares, each on or off for everyone. The operator reads and sets them from the console's new Flags page and from `sock-admin flags`.
- The first flag is `open_registration`, off by default. On, a person registers without an invite; off, everything is as today. Flipping it takes effect on every replica within the `NOTIFY`'s latency, with no restart.
- Anonymous callers can ask whether an invite is required, so the apps, the website and `sock login` show the right screen.
- Room is left for targeting a flag at individual accounts: evaluation takes an account, and nothing here precludes an overrides table. None is built.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Feature flag | One of the keys the hub declares, on or off for everyone. Not a rollout percentage and not per account | Stored (`identity.feature_flags`) |
| Flag key | The flag's name, lowercase `[a-z_]`, declared in the hub with its description. A build declares the keys it reads; a migration seeds each one | Code and the seed row |
| Flag set | The fact that an operator token set a flag to a value at a time: the `OperatorAction` with target `FlagSet` | Stored (audit) |
| Registration policy | `invite_only` or `open`: whether registering needs an invite. `open` exactly when `open_registration` is on | Derived from the flag |
| Admission (changed) | How a registration gets in: redeeming an invite, the operator's waiver, or the open policy | Not stored: the human is the trace |

## Domain model

Changes to [domain-model](domain-model.md). Unchanged objects are not repeated.

### Identity

#### FeatureFlag

Entity, aggregate root. One per declared key, created by the migration that declares it, never deleted while any build reads it.

| Field | Type | Meaning |
|---|---|---|
| `key` | `FeatureFlagKey` | Identity. The closed set, today `open_registration` |
| `enabled` | bool | The platform-wide value |

`description` is not a field: it is the key's, declared in code beside it, and the RPCs carry it from there.

Behaviors:

- `Set(enabled)` (`SetFeatureFlag`): writes the value, records `OperatorAction(setFeatureFlag, FlagSet(key, enabled))` in the same transaction and notifies `feature_flags`. Setting the current value succeeds and is recorded, as `MessageHold.Set` does.
- `Enabled(accountId)`: the value, read from the hub's memory. `accountId` is zero for an anonymous caller. Today the answer ignores it; account targeting would consult an override first.

Invariants: `key` is declared, and exactly one row exists per declared key (the migration that declares a key seeds it; `SetFeatureFlag` for an undeclared key is `FIELD_INVALID`). No state machine: a bool with no transition rules.

Relationships:

| With | Kind | Cardinality |
|---|---|---|
| `OperatorAction` (`setFeatureFlag`) | referenced by, through `FlagSet` | 1 to n |

#### FeatureFlagKey

Enumeration, declared in the hub: `open_registration` ("Register without an invite. Off: every registration redeems an invite, or is the operator's own Apple ID."). Adding a key is a code change and a migration, not an operator action.

#### RegistrationPolicy

Enumeration, derived: `open` when `FeatureFlag(open_registration).Enabled(0)`, else `invite_only`. Read by every registration and by `GetRegistrationPolicy`.

#### Admission (changed)

Value object, built by each registration from the invite code, the policy and, for Apple, the verified subject:

- A non-empty `invite_code` is always redeemed (`Invite.Redeem`), whatever the policy: a stale code given while open is `INVITE_INVALID`, never silently ignored.
- An empty `invite_code` is admitted when the policy is `open`, or when the verified Apple subject is the operator's (`SOCK_OPERATOR_APPLE_SUB`, as today). Otherwise `INVITE_INVALID`.

Email registration (`RegisterWithEmail`, ticket or AuthKit code) builds it the same way with no Apple subject, so the operator's waiver never applies there and the open policy does.

#### OperatorAction (changed)

`action` gains `setFeatureFlag`; `target` gains `FlagSet(key, enabled)`, present exactly for it.

#### Evaluation (hub, not the model)

The hub keeps every flag in memory: loaded at start, reloaded in one query on each `feature_flags` notification and after the listener reconnects, which covers a notification missed while it was down. `Set` runs on the replica that took the call; every other replica learns of it from the notification. A replica whose listener is down keeps its last values until it reconnects: a flip is late there, never lost.

## Configuration

None new. The listener uses the hub's existing `pgnotify` connection.

## Caller classes

| Class | Authenticates with | Trusted to assert | Never trusted to assert |
|---|---|---|---|
| Operator (changed) | `sock_ot_` token, `full` or `read` | A flag key and a value | Who it is beyond the token: `FlagSet` records the token id |
| Anonymous (changed) | Nothing | Nothing | Anything; `GetRegistrationPolicy` reveals only what trying to register would |

## API

### OperatorService (added to [api](api.md))

| RPC | Caller | Authz | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|
| `GetFeatureFlags` | Operator | Any valid operator token, `full` or `read` | none | `flags`: every declared flag, key order, each with `key`, `description`, `enabled` and `last_set` (`at`, `operator_token_id`, `operator_token_label`; unset when no `setFeatureFlag` for the key is within the audit's 365 days) | none | A read; writes no action | none: a read |
| `SetFeatureFlag` | Operator | Any valid `full` operator token | `key` (a declared key), `enabled` | `flag`, the stored flag as `GetFeatureFlags` shows it | `FIELD_INVALID` (`key`: not declared) | Idempotent: the current value succeeds and still records an action | `FeatureFlag.Set(enabled)` with `OperatorAction(setFeatureFlag)` in the same transaction; notifies `feature_flags` |

`operator_token_label` is the token's label while its row exists (`console sign-in`, `sock-admin sign-in`, or a bootstrap token's `--label`) and empty once the token is swept 30 days after expiry; the id stays.

Proto, `operator.proto`:

```proto
rpc GetFeatureFlags(GetFeatureFlagsRequest) returns (GetFeatureFlagsResponse) {}
rpc SetFeatureFlag(SetFeatureFlagRequest) returns (SetFeatureFlagResponse) {}

message FeatureFlag {
  string key = 1;
  string description = 2;
  bool enabled = 3;
  // Unset: no set for this key within the audit's 365 days.
  FeatureFlagSet last_set = 4;
}
message FeatureFlagSet {
  google.protobuf.Timestamp at = 1;
  string operator_token_id = 2;
  // Empty once the token's row is swept.
  string operator_token_label = 3;
}
message GetFeatureFlagsRequest {}
message GetFeatureFlagsResponse { repeated FeatureFlag flags = 1; }
message SetFeatureFlagRequest { string key = 1; bool enabled = 2; }
message SetFeatureFlagResponse { FeatureFlag flag = 1; }
```

### IdentityService (added and changed)

| RPC | Caller | Authz | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|
| `GetRegistrationPolicy` (new) | Anonymous | None. Rate limited in a bucket of its own, 60 calls per minute per client network (the same network key as the sign-in bucket), never the sign-in bucket: website traffic from a NATed office must not starve sign-ins behind it. CORS for `SOCK_INVITE_ORIGINS`, as `RequestInvite`, so the website can call it | none | `invite_required`: true under `invite_only` | `RATE_LIMITED` | A read | `RegistrationPolicy` |
| `RegisterWithApple` (changed) | Anonymous | As today, with Admission (changed) in place of "a valid unexpired invite (or an empty `invite_code` … for the operator's subject)" | unchanged | unchanged | unchanged | unchanged | unchanged, with `Invite.Redeem` only for a non-empty code |
| `RegisterWithEmail` (changed) | Anonymous | As today, with Admission (changed): an empty `invite_code` is admitted under `open` | unchanged | unchanged | unchanged | unchanged | as `RegisterWithApple` |

`RequestInvite`, `ListInviteRequests`, `ApproveInviteRequests` and `DeclineInviteRequests` are unchanged and keep working while open: an approved code still redeems. The website stops showing the form (below).

Proto, `identity.proto`:

```proto
rpc GetRegistrationPolicy(GetRegistrationPolicyRequest) returns (GetRegistrationPolicyResponse) {}
message GetRegistrationPolicyRequest {}
message GetRegistrationPolicyResponse { bool invite_required = 1; }
```

## Domain events

None. Nothing outside Identity reacts to a flag: admission reads it in its own context. The `feature_flags` notification is cache invalidation, not a domain event: it carries no payload and every reader reloads from the table.

## Data layer

Migration `00033_feature_flags.sql`:

```sql
CREATE TABLE identity.feature_flags (
    key     text    PRIMARY KEY,
        -- natural key, the closed set the hub declares; a new flag is an
        -- INSERT in the migration that declares it, never an operator action
    enabled boolean NOT NULL
);
INSERT INTO identity.feature_flags (key, enabled) VALUES ('open_registration', false);

INSERT INTO identity.operator_action_kinds (name) VALUES ('setFeatureFlag');

CREATE TABLE identity.operator_action_feature_flags (
    action_id bigint  PRIMARY KEY REFERENCES identity.operator_actions (id) ON DELETE CASCADE,
    key       text    NOT NULL REFERENCES identity.feature_flags (key) ON DELETE RESTRICT,
        -- retiring a flag deletes its audit rows first, in that migration
    enabled   boolean NOT NULL
        -- the value set
);
CREATE INDEX operator_action_feature_flags_key_idx
    ON identity.operator_action_feature_flags (key, action_id DESC);
    -- serves FeatureFlag.last_set: the newest action for a key
```

`identity.assert_action_target` gains the new table's count, matched to `setFeatureFlag`, and the table gets the same deferred constraint trigger the other target tables have. The 365-day sweep of `operator_actions` cascades here, which is why `last_set` is bounded by it.

5NF: `feature_flags` is a key and one value; `description` stays in code because it is a property of the key's declaration, not a stored fact, and storing it would copy a constant. The audit table holds one fact per action and nothing derivable.

Not created: `identity.feature_flag_overrides (key, account_id, enabled)`, the shape account targeting would take. Tracked as an issue, not scaffolded.

## Console

A new tab, Flags, after Message hold. It calls `GetFeatureFlags` when it opens and renders one row per flag: the description, a switch, and "set on by `<label or id>` at `<local time>`" (or "never set"), with a Refresh button that reads again. Flipping a switch asks for confirmation first, naming the flag and the new value, then calls `SetFeatureFlag` and re-renders the row from the response. A `read` token's flip is refused as every console write with a `read` token is today (`CALLER_NOT_ALLOWED`, which signs the console out); the console cannot tell the scopes apart and does not try.

## sock-admin

Added to [admin-cli](admin-cli.md):

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock-admin flags list` | `GetFeatureFlags` | none | `<key>  on|off  <description>` per flag, then `  set by <label or id> at <local time>` when set; json: `FeatureFlag` per line |
| `sock-admin flags set <key> on\|off` | `SetFeatureFlag` | key, value | `<key> on|off`; json: `FeatureFlag`. Asks for confirmation on a terminal; off one, `--yes` |

## Website

The home page and `/invite` call `GetRegistrationPolicy` on load. Under `open`: "Request an invite" and "Sock is invite-only while we grow." are replaced by Download alone, and `/invite` says registration is open and links to Download. Under `invite_only`, or when the call fails: as today. The form stays deployed either way.

## Apps and CLI

The iOS, Mac and desktop registration screens call `GetRegistrationPolicy` and hide the invite field under `open`; their changes are the apps' own passes. `sock login` reads it before prompting ([CLI sign-in through AuthKit](cli-sign-in-through-authkit.md)).

## Threats

| Threat | Mitigation |
|---|---|
| Spam registrations once open | Every registration still needs an address a provider verified (`ProvesAddress`) or an Apple identity, the anonymous rate limit, and a handle. Abuse is answered by setting the flag off, which every replica honors within the notification's latency, and by suspension as today |
| A flag read by an anonymous caller | `GetRegistrationPolicy` reveals only `invite_required`, which trying to register would reveal anyway. The flag list and audit stay operator-only |
| A replica evaluating a stale value | Bounded by the listener's reconnect; the audit records the set time, so a late replica is explainable. Accepted: no registration is wrongly refused by staleness, only admitted a little late or a little long |
| A `read` token setting a flag | `SetFeatureFlag` needs `full`, as every operator write does |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `FeatureFlag.Set` | `SetFeatureFlag` | none: Identity's own row; the notification is invalidation | `feature_flags` update, `operator_actions` and `operator_action_feature_flags` insert |
| `FeatureFlag.Enabled` | `GetFeatureFlags`, `GetRegistrationPolicy` | none: a read | `feature_flags` |
| Admission under `open` | `RegisterWithApple`, `RegisterWithEmail` with empty `invite_code` | `HumanRegistered`, `DeviceAdded` as today | no `invites` delete; the human's rows as today |
| Flag declared | none: a migration | none | `feature_flags` insert |

## Open questions

- The confirmation wording for `open_registration` in the console and `sock-admin`: it should say what opens.
