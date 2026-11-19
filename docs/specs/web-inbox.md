# Web inbox

Pass 1, approved. Contexts: Identity (the browser session), Messaging (the read). Builds on [terminal sign-in](terminal-sign-in.md) (AuthKit from a hub page, the derived PKCE verifier, `webpage` headers) and [email sign-in, pass 3](email-sign-in-pass-3.md) (which WorkOS sign-ins the hub trusts).

A human signs in at `https://<hub host>/inbox` in any browser and sees the messages addressed to them that the hub still holds: those some device of theirs has not yet acked, and those waiting for a first device. Their own sent messages, which the hub delivers to their other devices, are left out. The page reads and nothing else. It never acks, leases, sends or marks read, so a human's apps receive every message they would have received without it.

Why: a person with no Sock app has no way to see that a message reached them. App store reviewers testing the ChatGPT app (`sock-mcpd`) send with `sock_send` and need to see it arrive. The hub keeps no history (ADR 1, ADR 56), so the inbox is the human's slice of the delivery queue, not a message archive.

Non-goals: sending or replying, puppet queues, read markers, billing, history of acked messages, live updates (the page is reloaded to refresh).

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Inbox session | One browser signed in to one human's inbox for one hour. Not a device: it gets no deliveries, no push, and appears in no device list | Stored (`identity.inbox_sessions`) |
| Inbox cookie | `__Host-sock-inbox`: 32 random bytes, base64url; the session is found by its SHA-256 | Stored as a hash |
| Inbox sign-in cookie | `__Host-sock-inbox-signin`: 32 random bytes, base64url, set by the signed-out page; AuthKit's `state` is the base64url SHA-256 of it, so only the browser that started a sign-in can finish it | Not stored: derived at the callback |
| Waiting message | A message the human did not send, with a device delivery to any endpoint of the human or an account delivery to the human. A message one device acked and another has not is still waiting: the hub holds it | Derived from `messaging.device_deliveries`, `messaging.endpoints`, `messaging.account_deliveries` |

## Domain model

Changes to [domain-model](domain-model.md). Unchanged objects are not repeated.

### Identity

#### InboxSession (new)

Entity, aggregate root. One browser's read access to one human's inbox.

Fields: `id` (UUID, made by the database), `secretHash` (32 bytes), `humanId`, `createdAt`, `expiresAt` (one hour after `createdAt`).

Behaviors:

- `Open(signedIn SignedInEmail)` (factory): resolves the WorkOS user to a human by `email_identities.provider_user_id` under the trust rule `SignInWithEmail` applies (a method that proves the address, or a password the hub trusts for that human), refuses a suspended or archived human, and returns the session and its cookie value, once. Deletes expired sessions first, so none is swept by a timer.
- `Resolve(cookieHash, now)`: the live session with that hash whose human is not suspended or archived. A suspended human's sessions are deleted on the first request that finds them.
- `Close()`: deletes the session (sign-out).
- `Account.Suspend`, `Operator.ArchiveHuman` and `Account.RemoveEmailIdentity` (changed): each deletes the human's inbox sessions in its own transaction, so a session cannot outlive a suspension or archive that is later lifted, nor the email sign-in it was opened with.

Invariants:

- The cookie value is shown once, in the `Set-Cookie` of the callback.
- A session belongs to exactly one human and dies with them (`ON DELETE CASCADE`).
- A session past `expiresAt` resolves to nothing, whether or not its row is still present.
- `Open` issues a session only where `SignInWithEmail` would issue a device for the same `SignedInEmail`; a WorkOS user with no human gets no session and no registration ticket.

Relationships: names a `Human`. Invokes nothing in Messaging.

### Messaging

#### Waiting messages (new read, no aggregate change)

`WaitingFor(humanId, limit)`: the waiting messages of a human, newest first, at most `limit` (200), each with conversation id, sender id, `sent_at`, seq and envelope. Writes nothing: it does not call `EnsureCaller`, take a lease or touch a delivery. Blocks are applied when deliveries are written (ADR 4), and only then: a delivery from a sender blocked afterwards stays, as it does for `FetchPending`, and the read does not filter it. Messages the human sent are excluded (`sender_id <> humanId`): `Post` delivers them to the sender's other devices, and they are not waiting for the human.

A message to a human with several devices, delivered to some and not others, is shown once.

### Application service

`ShowInbox(session)`: `InboxSession.Resolve`, then `WaitingFor(session.humanId)`, then Identity's account lookup (the `GetAccounts` rules, including the system puppet owner rule, seen as the session's human) for every sender, every member of every listed conversation the human still belongs to and the owner of every non-system puppet among them, then the conversation members (`Members`), the human's own memberships and direct pairs from Messaging. A conversation the human has left or was removed from shows no members: deliveries outlive membership, and its current members are not the human's to see. The system puppet owner rule moves out of `internal/api`'s proto conversion to one function both callers use; it is not copied. No rule lives here: it composes reads from two contexts.

## Configuration

| Variable or setting | Meaning |
|---|---|
| WorkOS dashboard, per environment | `https://<hub host>/workos/inbox-callback` registered as a redirect URI beside the terminal callback |

The routes reuse terminal sign-in's configuration and its `404` rule: with WorkOS unconfigured or no hub host, every route below answers `404`. No new variable.

## Caller classes

Changes to [api](api.md), Caller classes. Anonymous gains `GET /inbox` signed out and `GET /workos/inbox-callback`. A new class:

| Class | Authenticates with | Trusted to assert | Never trusted to assert |
|---|---|---|---|
| Inbox browser | `__Host-sock-inbox` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Max-Age=3600`), whose SHA-256 names a live session of an unsuspended human. Reaches `GET /inbox` and `POST /inbox/sign-out` only | That it is the browser a WorkOS user signed in from within the last hour | Which human it is (only the session row says so); anything about other humans; any write to Messaging |

## API

### Hub routes (new, [api](api.md), Operational)

On every listener. Every response carries `webpage.Headers` with `Cache-Control: no-store`, `Referrer-Policy: same-origin` and the pages' CSP with no script (`webpage.CSP("'self'")`). Nothing logs a cookie, a code or a query string; the routes log the session id, the human id and the outcome. Anything that breaks a rule below is the fixed `400` page with nothing echoed.

| Route | Request | Response |
|---|---|---|
| `GET /inbox` | No body. Optional `__Host-sock-inbox` | A cookie naming a live session: draws the session's inbox bucket (6 a minute, in memory per replica, as ADR 46's limits); over it, `429` page with `Retry-After`; else `200`, the inbox page (below). No cookie, or one naming nothing: the request's `__Host-sock-inbox-signin` reused when present and well formed, else a fresh one (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Max-Age=900`), any stale `__Host-sock-inbox` expired, and `200` page: "Sign in to see messages waiting for you" with one link to WorkOS's `/user_management/authorize`, `provider=authkit`, `screen_hint=sign-in`, `max_age=0` (AuthKit asks for a fresh sign-in even when its own session is live, so a shared browser never hands over the previous WorkOS user), `redirect_uri` the inbox callback, `state` = base64url(SHA-256(the sign-in cookie)), and the challenge of `InboxVerifier(state)`. A session whose human is now suspended or archived: deleted, cookie expired, `403` page "This account is suspended". Not rate limited: it reads one row by a unique hash |
| `GET /workos/inbox-callback` | WorkOS's redirect: `state` (43 base64url characters), and either `code` (1 to 512 printable ASCII) or `error` matching `^[a-z_]{1,64}$`, never both. Draws the anonymous network bucket | Only when the request carries `__Host-sock-inbox-signin` whose SHA-256, base64url, equals `state` (constant time), checked before the code is spent or the `error` branch is taken; without that, the fixed `400`. `code`: redeemed with WorkOS (5 seconds) using `InboxVerifier(state)`; `InboxSession.Open`; any session the request's old `__Host-sock-inbox` named is closed; `__Host-sock-inbox` set, the sign-in cookie expired, `303` to `/inbox`. `error`: the sign-in cookie expired, `200` page "Sign-in was cancelled" with a link back to `/inbox`. WorkOS unreachable: `503` page. A WorkOS user with no human, or a password the hub does not trust: `200` page "No Sock account uses that sign-in", which says a person who signed up with Apple in the app adds email sign-in there first (ADR 37), with a link to `https://joinsock.com`; sign-in cookie expired. Suspended or archived human: `403` page. A refused code, or an impersonated session (`ErrImpersonated`): `400`, sign-in cookie expired |
| `POST /inbox/sign-out` | Form, body at most 16 KiB, no fields. `Origin`, when sent, must be `https://<hub host>`. `__Host-sock-inbox` | `InboxSession.Close` for the session the cookie names, if any; cookie expired; `303` to `/inbox`. Always the same answer, so it reveals nothing |

### The inbox page

- A header naming the signed-in human (`@handle`, display name), the time the page was built (UTC), and a Sign out form (`POST /inbox/sign-out`).
- One line explaining the scope: "Messages the hub is still holding for you. Messages your Sock apps have already received are not kept and do not show here."
- The waiting messages, newest first, grouped by conversation in order of each conversation's newest message. A conversation is labeled by its other member for a direct conversation, by up to three member names and a count for a group, and "A conversation you left" when the human is no longer a member. Each message shows the sender's display name and `@handle` (a puppet: its name and `@owner`'s handle; a system puppet, whose owner the rule hides: its name and "Sock"), `sent_at` in UTC, and the body.
- A `text/v1` body, and every display name and handle, has C0 and C1 control characters other than newline and tab, DEL, the directional marks U+200E, U+200F and U+061C, the separators U+2028 and U+2029, and the bidirectional controls U+202A to U+202E and U+2066 to U+2069, replaced with U+FFFD (the zero width joiner stays, for emoji) before rendering, so no body can fake a header, a sender or another message. It is then rendered as text through `html/template`, with a `pre-wrap` class added to `internal/webpage/page.css` (the CSP admits only that stylesheet by hash, so no inline style); no markup, link or image in a body is ever interpreted. Any other scheme shows "A message this page cannot show".
- At the 200 limit, a line says older waiting messages are not shown.
- With none: "Nothing is waiting for you."

### Rate limits

Added to [api](api.md): `GET /workos/inbox-callback` draws the anonymous network bucket, as the terminal callback does. A signed-in `GET /inbox` draws its session's inbox bucket, 6 a minute: it runs `WaitingFor` and up to a few hundred account lookups. A signed-out `GET /inbox` and `POST /inbox/sign-out` draw nothing: each is one indexed read or delete by a 256-bit hash.

No RPC is added or changed, so the proto is unchanged and `TestEveryRPCHasBlackBoxCoverage` is unaffected.

## Domain events

None. An inbox session is not a device, so no `DeviceAdded`; it is private to Identity and leaves no trace once deleted. Messaging learns nothing from an inbox read.

## Data layer

One table in the `identity` schema. One query in the `messaging` schema, served by existing indexes.

```sql
CREATE TABLE identity.inbox_sessions (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
        -- not in the cookie: the cookie is found by its hash alone
    secret_hash bytea       NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
        -- SHA-256 of __Host-sock-inbox. Serves Resolve and Close
    human_id    uuid        NOT NULL REFERENCES identity.humans (account_id) ON DELETE CASCADE,
        -- CASCADE: erasing a human ends their sessions
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
        -- one hour after created_at, set by Open
    CHECK (expires_at > created_at)
);
CREATE INDEX inbox_sessions_expires_idx ON identity.inbox_sessions (expires_at);
    -- serves Open's sweep, DELETE WHERE expires_at < now()
CREATE INDEX inbox_sessions_human_idx ON identity.inbox_sessions (human_id);
    -- serves the FK cascade on erase
```

`WaitingFor`, driven from the delivery tables so it never scans `messaging.messages`:

```sql
SELECT m.id, m.conversation_id, m.sender_id, m.seq, m.sent_at, m.envelope_scheme, m.envelope
FROM messaging.messages m
JOIN (SELECT d.message_id FROM messaging.device_deliveries d
      JOIN messaging.endpoints e ON e.device_id = d.device_id
      WHERE e.account_id = $1
      UNION
      SELECT a.message_id FROM messaging.account_deliveries a
      WHERE a.account_id = $1) w ON w.message_id = m.id
WHERE m.sender_id <> $1
ORDER BY m.sent_at DESC, m.conversation_id, m.seq DESC
LIMIT $2;
```

`UNION`, not `UNION ALL`: a message owed to several devices is shown once. `endpoints_account_idx`, `device_deliveries`'s primary key `(device_id, message_id)`, `account_deliveries`'s primary key `(account_id, message_id)` and `messages`' primary key serve it. The builder confirms the plan with `EXPLAIN` against a seeded database.

## Threats

| Threat | Mitigation |
|---|---|
| A code issued for another flow (OAuth consent, terminal sign-in) redeemed here | `InboxVerifier` derives the PKCE verifier under its own label (`sock workos pkce inbox`), so no other flow's challenge matches it, whatever shape its request id has |
| Login CSRF: an attacker's own AuthKit callback URL sent to a victim, signing the victim's browser into the attacker's inbox | The callback requires `__Host-sock-inbox-signin` whose hash is `state`; the attacker cannot set a `__Host-` cookie in the victim's browser, so the callback is refused before the code is spent |
| A WorkOS code replayed into another sign-in | The PKCE challenge is derived from `state`; WorkOS refuses the code with another state's verifier |
| A stolen inbox cookie | `HttpOnly`, `Secure`, `__Host-`; one hour; read only. The holder reads that human's waiting messages until expiry or sign-out. Accepted as the cost of a browser session; the same cookie cannot send, ack or change anything |
| Message content injecting markup into the page | Bodies go through `html/template` as text; the CSP allows no script, no frame and no foreign form target |
| A cross-site page reading the inbox | `SameSite=Lax` sends the cookie on a top-level `GET`, but the response is a page the attacker's origin cannot read; `frame-ancestors 'none'` stops framing |
| A cross-site sign-out | Harmless, and `POST /inbox/sign-out` refuses a foreign `Origin` anyway |
| The inbox changing what devices receive | `WaitingFor` is a plain `SELECT`; it does not call `EnsureCaller`, and the session is no endpoint, so `Post` and backfill never see it |
| A password sign-in for an address nobody verified | `Open` applies `SignInWithEmail`'s trust rule; an untrusted password gets the "No Sock account uses that sign-in" page |
| A suspended human keeping read access | `Suspend` deletes the human's inbox sessions in its transaction; `Resolve` also checks suspension and archive on every request |
| The next person on a shared browser landing in the previous human's inbox through AuthKit's own session | Every inbox sign-in link carries `max_age=0`, so AuthKit asks for credentials whatever session it holds; the page header names the signed-in human. A browser left signed in to the inbox keeps that session for its hour: the accepted residual of any web sign-in |
| Message content or names faking page structure with control or bidi characters | Replaced with U+FFFD before rendering |
| A removed member reading a group's current membership | Members are shown only for conversations the human still belongs to |
| Reloading the inbox as a load attack | The per-session bucket, and `WaitingFor` reads only the human's deliveries |
| An AuthKit code left unspent in a URL after a refused callback (no or wrong sign-in cookie) | Needs a second leak of the URL within the code's life; then the code redeems only with the matching verifier, which needs the sign-in cookie's hash as `state`. Accepted, as for terminal sign-in |
| Message content in logs or caches | Routes log ids and outcomes only; `Cache-Control: no-store` |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `InboxSession.Open` | `GET /workos/inbox-callback` | none: not a device | `identity.inbox_sessions` insert, expired rows deleted |
| `InboxSession.Resolve` | `GET /inbox` | none: a read | `identity.inbox_sessions` select; delete when suspended |
| `InboxSession.Close` | `POST /inbox/sign-out` | none | `identity.inbox_sessions` delete |
| `Account.Suspend`, `Operator.ArchiveHuman`, `Account.RemoveEmailIdentity` (changed) | their RPCs and CLI paths (unchanged) | unchanged | `identity.inbox_sessions` delete of the human's rows |
| Human erased | `EraseAccount` (unchanged) | `AccountErased` (unchanged) | `identity.inbox_sessions` cascade |
| `WaitingFor` | `GET /inbox` | none: a read | `messaging.messages` select |

## Tests

Black-box tests under `blackbox/`, with the fake WorkOS and the `browserDo` helpers terminal sign-in uses:

- Signed-out `GET /inbox` sets the sign-in cookie and links AuthKit with `state` and `code_challenge` derived from it.
- A full sign-in shows a message a puppet sent to a human with no endpoint (account delivery), and a message to a human with an endpoint that has not acked (device delivery).
- Viewing the inbox, then `FetchPending` on the human's device, returns the same messages: the inbox consumed nothing.
- A message acked by every device of the human does not show.
- A callback without the sign-in cookie, or with another cookie, is `400` and does not spend the code.
- A WorkOS user with no human gets the "No Sock account uses that sign-in" page and no session cookie.
- A message the human sent from one device does not show, though their other device has not acked it.
- Two signed-out loads of `GET /inbox` share one sign-in cookie, so the first tab's link still completes.
- A callback carrying `error` without the sign-in cookie is the fixed `400`.
- Sign-out ends the session; the old cookie gets the signed-out page.
- Suspending the human deletes their inbox sessions; the old cookie gets the signed-out page.
- A removed group member sees the group's waiting message labeled "A conversation you left", with no members.
- A body and a display name carrying U+202E render with U+FFFD in its place.
- A seventh signed-in `GET /inbox` within a minute is `429`.
- The AuthKit link carries `max_age=0`.
- A system puppet's message shows its name and "Sock", never its owner.
- A body containing `<script>` renders escaped.
- An expired session gets the signed-out page (move `created_at` and `expires_at` back together with `psql`, as `blackbox/operator_test.go` does, or the `CHECK` refuses the update).

## Open questions

`sock-mcpd`'s tools do not name the inbox: the ChatGPT app's reviewer notes carry the URL. A link in `sock_send`'s result would show an empty page to anyone whose app already took the message.


