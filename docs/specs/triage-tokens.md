# Triage tokens

Approved 2026-11-02. Contexts: Identity. The hub half of sock-triage's [feedback console](https://github.com/aeryx-ai/sock-triage/blob/main/docs/specs/feedback-console.md), which carries the triage and console halves. Decision record: sock-server `docs/adr/contract/0049-triage-tokens-are-derived-operator-tokens.md`.

A signed-in console reaches sock-triage with a token the hub mints and triage checks with the hub. That token is an operator token of a new scope, `triage`, derived from the console's `full` token. It reaches no hub RPC that reads or acts.

## Domain

### OperatorToken (entity, changed)

Gains the scope `triage`. Fields unchanged; a triage token also owns a `TokenDerivation`.

- **Admits exactly** `VerifyTriageToken` and `RevokeOperatorToken`, each for the presenting token only. Every other procedure refuses it with `CALLER_NOT_ALLOWED`, through the same allowlist as `read` (ADR 15): a procedure added later refuses `triage` until it is listed.
- **`MintTriage(parent)`**, the only way to a triage token, from `MintTriageToken` under a live `full` token. `sockd operator token mint --scope triage` is refused: a triage token always has a parent.
- **Expiry:** the earlier of the parent's `expiresAt` and 12 hours after mint, by the database clock.
- **Revocation:** revoking a token revokes every triage token derived from it, in the same statement, whoever writes the revocation: an `AFTER INSERT` trigger on `operator_token_revocations` revokes the token's children at the parent's `revoked_at`, so `RevokeOperatorToken`, `sockd operator token revoke`, an older binary and a stale CLI all cascade. Revoking a triage token leaves its parent alone. A revoked triage token stops working at triage within triage's 60 s verification cache.

### TokenDerivation (value object, new)

`child_id` (the triage token) and `parent_id` (the `full` token that minted it). It exists so the parent's revocation can cascade. Each child has exactly one parent; a parent has 0 to n children. Deleted with either token.

### OperatorAction (changed)

New kind `mintTriageToken`, written in the mint's transaction. Its `operator_token_id` is the parent, the caller; its target, `TokenMint`, names the child. The audit outlives the token sweep, so a triage token's id still ties to the full token that minted it after both rows and the derivation are gone.

## Caller classes

| Class | Authenticates | Trusted to assert | Never trusted to assert |
|---|---|---|---|
| Console operator | `Authorization: Bearer` a `full` operator token (`SignInOperatorWithApple`) | that it is the operator | anything a triage token would add: it mints one, it is not one |
| Triage daemon | `Authorization: Bearer` the console's `triage` token, presented to check it | nothing: it relays the hub's answer | that a token is valid without the hub saying so |

## API, `OperatorService`

Every refusal of a token (malformed, unknown, expired, revoked or the wrong scope) is the operator path's one fixed `PERMISSION_DENIED` `CALLER_NOT_ALLOWED` (api.md, Caller classes), never `UNAUTHENTICATED`. Both RPCs draw from the service auth bucket like every `OperatorService` RPC.

| Field | `MintTriageToken` | `VerifyTriageToken` |
|---|---|---|
| Caller | console operator | triage daemon, presenting the console's token |
| Authorization | scope `full` | scope `triage`, and the token checks only itself |
| Request | `{}` | `{}` |
| Response | `token` (`sock_ot_`, shown once, SHA-256 stored), `expires_at` | `token_id`, `expires_at`: the presenting token's |
| Errors | `CALLER_NOT_ALLOWED` (no live `full` token), `RATE_LIMITED` | `CALLER_NOT_ALLOWED` (no live `triage` token), `RATE_LIMITED` |
| Idempotency | not idempotent: each call mints a token. The console calls it once per sign-in | a read, safe to retry; writes no action |
| Domain behavior | `OperatorToken.MintTriage(parent)`: the token, its `TokenDerivation`, and `OperatorAction(mintTriageToken)` with its `TokenMint` target, in one transaction under a share lock on the parent | the token lookup, nothing more |

`RevokeOperatorToken` (existing) admits `triage` and gains the cascade. At sign-out the console revokes its full token, which takes the triage token with it.

## Domain events

None. `TriageTokenMinted` and `TriageTokenRevoked` are deliberately absent: the mint is in the operator audit like every network write, and nothing reacts to either.

## DDL: sock-server `migrations/00034_triage_tokens.sql`

```sql
INSERT INTO identity.operator_token_scopes VALUES ('triage');

CREATE TABLE identity.operator_token_derivations (
    child_id  uuid PRIMARY KEY REFERENCES identity.operator_tokens (id) ON DELETE CASCADE,
        -- a triage token; one parent each
    parent_id uuid NOT NULL REFERENCES identity.operator_tokens (id) ON DELETE CASCADE
        -- the full token that minted it
);
CREATE INDEX operator_token_derivations_parent_idx ON identity.operator_token_derivations (parent_id);
    -- serves the revocation cascade: children of $1

INSERT INTO identity.operator_action_kinds (name) VALUES ('mintTriageToken');
CREATE TABLE identity.operator_action_token_mints (
    action_id bigint PRIMARY KEY REFERENCES identity.operator_actions (id) ON DELETE CASCADE,
    token_id  uuid   NOT NULL
        -- the triage token minted. No FK, like operator_actions.operator_token_id:
        -- the audit outlives the swept token rows
);
```

`operator_token_revocations` gains an `AFTER INSERT` trigger that inserts a revocation, at `NEW.revoked_at`, for every child of `NEW.token_id` in `operator_token_derivations`, `ON CONFLICT (token_id) DO NOTHING`. The target table carries the operator audit's two triggers: the deferred constraint trigger matching target to kind (`identity.assert_action_target` gains `mintTriageToken`) and the append-only `UPDATE` refusal. Down drops the cascade trigger, deletes every `triage` token first, since before 00034 an unknown scope cannot be read, and fires the deferred trigger with `SET CONSTRAINTS ... IMMEDIATE` before dropping the target table, as 00033 does.

The rule that a `triage` token has a derivation whose parent is `full` holds in the mint, not in a constraint: a cross-row check would need a trigger, and `MintTriage` is the only writer. Recorded reason: a single writer, covered by a test asserting no `triage` token exists without a derivation.

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| Triage token minted | `MintTriageToken` | none: audited in `operator_actions` | `operator_tokens`, `operator_token_derivations`, `operator_action_token_mints` |
| Triage token verified | `VerifyTriageToken` | none: a read | none: a read |
| Triage token revoked | `RevokeOperatorToken` (itself, or its parent), `sockd operator token revoke` | none | `operator_token_revocations` (existing), cascade over `operator_token_derivations` |
