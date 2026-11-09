# Sign-in identities

Pass 1. Built on the owner's instruction from the 2026-11-08 identity security review (sock-server bd `sock-server-cap`); awaiting his read. Contexts: Identity. Builds on [email sign-in, pass 2](email-sign-in-pass-2.md) (`AddEmailSignIn`) and [pass 3](email-sign-in-pass-3.md) (`PasswordSignIn`), which stay in force.

What changes:

- A signed-in human can see how they sign in: Apple, and the email sign-in with the address WorkOS holds for it.
- A human who signs in with Apple too can remove their email sign-in.

Why: `AddEmailSignIn` binds a WorkOS user to the caller on a session alone. Anyone holding a stolen session could bind their own address, and the human had no way to see it or take it away: every later emailed code to that address signed in to the account, after every device was revoked. Seeing and removing it makes the binding as recoverable as a passkey already is. Whether adding one needs a fresh sign-in is the open decision passkeys share (sock-server `sock-server-alf`).

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Sign-in identity | Unchanged from [email sign-in](email-sign-in.md): what a human proves to sign in, an Apple identity or an email identity. A passkey is not one: it never stands alone | Stored |
| Email sign-in | A human's email identity as the human sees it: the address WorkOS holds for the WorkOS user | The identity stored; the address read from WorkOS, never stored |

## Domain model

### Account (changed behaviors)

Entity, aggregate root.

- `SignInIdentities()` (`ListSignInIdentities`, a read): whether the human has an Apple identity, and their email identity. The address is the adapter's read of the WorkOS user (`EmailSignIn.UserEmail`), made on each call; when WorkOS is not configured, does not answer or no longer holds the user, the identity is returned with no address rather than hidden, so a human always sees that it exists.
- `RemoveEmailIdentity()` (`RemoveEmailSignIn`): deletes the human's email identity, and with it its `PasswordSignIn`. Refused unless the calling session was issued in the last 10 minutes (`RECENT_SIGN_IN_REQUIRED`): `AddEmailSignIn` refuses a human who already has email sign-in, so without this a stolen session could remove the human's own email sign-in and add the thief's. Refused when the human has none (`NOT_FOUND`) and when it is the human's only sign-in identity (`LAST_SIGN_IN_IDENTITY`); the human's row is locked as `AddEmailIdentity` locks it, so the two serialize. The WorkOS user is left at WorkOS: it holds no human afterwards, as a WorkOS user an unregistered email code makes does, and the person can add it again with a code. Sessions already issued stay until the human revokes them (`ListDevices`, `RevokeDevice`): no session records the identity it was issued through.

Invariants, unchanged and enforced as before: a human holds at least one sign-in identity (the deferred `email_identity_keeps_one` trigger, `00025`), at most one of each kind, and an identity belongs to at most one human.

### EmailSignIn (anti-corruption layer, changed)

Gains `UserEmail(user) (address, error)`, which `internal/workos` already implements for the trial reminder (`GET /user_management/users/<id>`).

## Caller classes

Unchanged. Human device reaches the two new RPCs; a puppet token and a machine key are `CALLER_NOT_ALLOWED`; Anonymous reaches neither.

## API

### IdentityService (new RPCs)

| RPC | Caller | Authn | Authz | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|---|
| `ListSignInIdentities` | Human device | `sock_st_` session | Self. Terms-exempt (a read). 1 a minute per account, burst 5, in each replica's memory | empty | `apple` (bool), `email` (`EmailSignIn { address }`, unset when none; `address` empty when WorkOS could not be read) | `CALLER_NOT_ALLOWED`, `RATE_LIMITED` | Read | `Account.SignInIdentities()` |
| `RemoveEmailSignIn` | Human device | `sock_st_` session issued in the last 10 minutes (the device's `created_at`) | Self. Terms-exempt (a safety path): the human has an email identity and an Apple identity | empty | empty | `RECENT_SIGN_IN_REQUIRED` (checked first), `NOT_FOUND`, `LAST_SIGN_IN_IDENTITY`, `CALLER_NOT_ALLOWED` | Idempotent in effect: a retry after success is `NOT_FOUND` | `Account.RemoveEmailIdentity()` |

`errors.proto` gains `ERROR_REASON_LAST_SIGN_IN_IDENTITY` and `ERROR_REASON_RECENT_SIGN_IN_REQUIRED` (both `failed_precondition`). The app answers `RECENT_SIGN_IN_REQUIRED` by signing in again (Apple or email) and retrying with the new session.

The hub logs `AddEmailSignIn` and `RemoveEmailSignIn` at Info with the account id and the WorkOS user id, never the address.

## Domain events

None. No context besides Identity reads email identities, as for `AddEmailSignIn` ([pass 2](email-sign-in-pass-2.md)).

## Data layer

No table changes. `RemoveEmailIdentity` is `DELETE FROM identity.email_identities WHERE human_id = $1`, after locking the human's account row `FOR UPDATE` and checking `identity.apple_identities` under it; `identity.password_sign_ins` cascades. The deferred trigger refuses a removal that would leave no identity even if the check were skipped.

## Apps

Settings, "Sign-in methods": Apple when `apple`; email with its address when `email` is set ("Email sign-in (address unavailable)" when the address is empty), with Remove when `apple` is true too. Remove signs the person in again first when the session is older than 10 minutes. After Remove, the devices list, so the person can revoke sessions they do not recognize. Each app's own pass; no app change ships with this spec.

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `Account.SignInIdentities()` | `ListSignInIdentities` | none: a read | reads `identity.apple_identities`, `identity.email_identities` |
| `Account.RemoveEmailIdentity()` | `RemoveEmailSignIn` | none: Identity's own row, as `AddEmailIdentity` | `identity.email_identities` delete, `identity.password_sign_ins` cascade, `email_identity_keeps_one` checked at commit |

## Completeness gate

- Both new RPCs have every field; the one new error reason is in the closed taxonomy.
- No event added.
- No table added or changed.
- Caller classes: Human device reaches both; none other.

## Threats

| Threat | Mitigation |
|---|---|
| A stolen session binds the thief's inbox with `AddEmailSignIn` | Visible in `ListSignInIdentities` and removable with `RemoveEmailSignIn`. Prevention needs a fresh sign-in on add, the open question below |
| A stolen session removes the human's own email sign-in, then adds the thief's | `RemoveEmailSignIn` needs a session issued in the last 10 minutes; a thief with an older session cannot, and one with a fresh session could already add an inbox to a human without email |
| A stolen session reads the human's address | Accepted: the session already reads every message |

## Open questions

- Step-up on `AddEmailSignIn` (and `StartPasskeyRegistration`): a fresh sign-in or a notice to other devices. sock-server `sock-server-alf`.
- Revoking every other session when email sign-in is removed: no session records how it was issued, so the hub cannot tell an attacker's from the person's.
