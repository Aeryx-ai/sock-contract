# Terminal sign-in

Pass 2 (pass 1's callback approved the sign-in by itself; a security review showed a cross-site page could walk a victim through AuthKit into approving an attacker's terminal, so approval is now its own same-origin step after sign-in, as OAuth consent is). Built on the owner's instruction while he was away; awaiting his read. Contexts: Identity. Replaces the loopback listener of [CLI sign-in through AuthKit](cli-sign-in-through-authkit.md) for `sock login`; that spec's registration path (`SignInWithEmail` issuing a ticket, the terminal prompting for a handle, a name, the terms and an invite) stays in force and is reached from here. [Email sign-in, pass 3](email-sign-in-pass-3.md) (the registration ticket) and [feature flags](feature-flags.md) (the registration policy) stay in force.

What changes:

- `sock login` no longer listens on `127.0.0.1`, so it works where the browser is not on the machine running it: a headless box over ssh, a container, a devbox. It asks the hub to start a terminal sign-in, shows a short code and the hub page that takes it, opens the browser to that page when it can, and polls the hub until a browser has signed in with the code. The person signs in on any device.
- The browser side is the hub's own page, then AuthKit's hosted page with every provider WorkOS has enabled, then a hub callback that redeems the code and shows who would be signed in to which terminal, then one Approve button. The hub redeems the AuthKit code itself, as it does at OAuth consent; the terminal never holds a WorkOS code. Nothing before the Approve approves anything.
- `--port` is gone. `--no-browser` prints the page and the code without trying to open a browser; without it, the page and the code are printed too, since the browser may be elsewhere.
- `sock register` is removed as the AuthKit spec said: a sign-in that reaches a WorkOS user with no human gets a registration ticket, and `sock login` registers with it.
- The `cli` Sign in with Apple routes (`/apple/cli-login`, `/apple/cli-callback`) stay for `sock-admin login`, whose operator is matched on Apple's subject. The desktop Google relay stays for the desktop app, whose browser is on the same machine.

The shape is RFC 8628's device authorization grant, named in sock's terms: the terminal holds a secret (the device code), the person types a short code (the user code), the browser approves, the terminal collects the result by polling.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Terminal sign-in | One `sock login` waiting to be signed in: a secret only the terminal holds, a code the person types, the device label the session will carry, and a 10 minute life | Stored (`identity.terminal_sign_ins`) |
| Terminal token | `sock_tl_<id>_<secret>`: the terminal's claim on its sign-in, shown once by `StartTerminalSignIn`, hashed at rest | Stored as a hash |
| User code | 8 letters from `BCDFGHJKLMNPQRSTVWXZ` (20 letters, no vowels, so no word is a code), shown as `XXXX-XXXX`; about 34 bits. Entered by the person; case, spaces and dashes ignored | Stored |
| Claim | A browser has entered the code: the sign-in is bound to that browser by a cookie, so only it can finish the sign-in | Stored (`identity.terminal_sign_in_claims`) |
| Answer | AuthKit returned to the claiming browser: the WorkOS user it signed in, whether the address is verified, the method, and the hash of the single-use token in the approve form it was shown | Stored (`identity.terminal_sign_in_answers`) |
| Approval | The claiming browser posted the approve form from the hub's own origin: the person read who and which terminal and said yes | Stored (`identity.terminal_sign_in_approvals`) |
| Finish | The terminal presents its token after an approval: a session for the user's human, or a registration ticket when there is none; the sign-in is deleted | Not stored: the session or the ticket is the trace |

## Domain model

Changes to [domain-model](domain-model.md). Unchanged objects are not repeated.

### Identity

#### TerminalSignIn (new)

Entity, aggregate root. One `sock login` between `Start` and `Finish` or expiry.

Fields: `id` (UUID, in the token), `secretHash` (32 bytes), `userCode` (8 letters), `deviceLabel` (1 to 64 characters, the label `Finish` issues the session with), `createdAt`, `expiresAt` (10 minutes after `createdAt`), `polledAt` (the last `Finish`; paces polling), an optional `Claim` (`browserHash`, 32 bytes, the SHA-256 of the cookie; `claimedAt`), an optional `Answer` (`SignedInEmail`: the WorkOS user, verified, method; `approveTokenHash`, 32 bytes; `answeredAt`), only with a claim, and an optional `Approval` (`approvedAt`), only with an answer.

Behaviors:

- `Start(label)` (factory): a fresh secret and a fresh user code; returns the sign-in and the terminal token, once. Retries the code on a collision with a live sign-in.
- `Claim(browserHash)`: records the browser that entered the code. Refused once answered (the sign-in is someone's already) or expired. A second claim replaces the first and takes with it anything the first earned: the newest browser wins, and the older one's callback is refused.
- `Answer(browserHash, signedIn, approveTokenHash)`: records AuthKit's answer when the hash matches the claim's in constant time. Refused without a claim, with another browser's hash, once answered, or expired.
- `Approve(browserHash, approveToken)`: the person's yes, from the claiming browser, with the token the answer's page carried (compared by hash in constant time). Refused without an answer, with another browser's hash, a wrong token, once approved, or expired.
- `Finish(secret, now)`: the terminal's collection. The secret must match in constant time before expiry; a call sooner than the poll interval after the last is `RATE_LIMITED`; without an approval the answer is pending; with one, the sign-in is spent in the same transaction that issues the session or ticket. A refusal the answer earns (an untrusted password, a suspended human, an unverified address) spends it too.

Every mutation locks the sign-in row first and reads its claim, answer and approval afterwards, so none decides on a stale child.

Invariants:

- A terminal token is shown once, at `Start`.
- A sign-in is finished at most once: `Finish` deletes it.
- An answer exists only with a claim, from the browser that made the claim; an approval only with an answer, from that browser, with that answer's token.
- No live sign-in shares a user code with another (`UNIQUE`); a spent or expired one holds none, since it is deleted.
- `Finish` issues a session only for a verified sign-in method or a password the hub trusts, exactly as `SignInWithEmail` and `SignInWithGoogle` would for the same `SignedInEmail`.

Relationships: `Approval` names a WorkOS user (not a row here); `Finish` invokes `Device.Issue` for the user's human or `RegistrationTicket.Issue`.

#### AuthMethod (changed)

Gains `AppleOAuth`, from WorkOS's `authentication_method` for Sign in with Apple on AuthKit's page. `ProvesAddress` is true for it, as [CLI sign-in through AuthKit](cli-sign-in-through-authkit.md) decided; its open question (whether WorkOS marks the address verified) is checked by the first real Apple sign-in on the canary, and `RegistrationTicket.Issue` refuses an unverified address regardless.

## Configuration

| Variable or setting | Meaning |
|---|---|
| WorkOS dashboard, per environment | `https://<hub host>/workos/terminal-callback` registered as a redirect URI beside the desktop and OAuth callbacks |
| `SOCK_WORKOS_CLIENT_ID`, `SOCK_WORKOS_API_KEY`, `SOCK_APPLE_WEB_REDIRECT_URI` | Unchanged and reused: the hub host is that URI's host, as for the OAuth routes; the callback is `https://<hub host>/workos/terminal-callback`. With WorkOS unconfigured or no hub host, both RPCs answer `unavailable` and the three routes `404` |

No new variable.

## Caller classes

Changes to [api](api.md), Caller classes. Anonymous gains the two RPCs (`StartTerminalSignIn` in the sign-in bucket, `FinishTerminalSignIn` paced per sign-in) and the three routes below. A new class:

| Class | Authenticates with | Trusted to assert | Never trusted to assert |
|---|---|---|---|
| Signing-in browser | Nothing at `GET /login`; the user code at `POST /login` (a same-origin form: `Origin`, when sent, is the hub), which sets `__Host-sock-terminal` (32 random bytes, base64url; `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Max-Age=600`); that cookie plus AuthKit's code at `GET /workos/terminal-callback`, which the hub redeems with its own PKCE verifier; that cookie plus the answer page's single-use token at `POST /workos/terminal-approve` (same-origin). Reaches only those four routes | That the person holding it saw the code on the terminal it approves, is the WorkOS user AuthKit signed in, and said yes on the hub's own page | Which sign-in it finishes (only the code and the cookie say so); which WorkOS user it is (only the redeemed code says so); that it approved (only the same-origin POST with the token says so) |

## API

### Hub routes (new, [api](api.md), Operational)

Anonymous, on every listener. All `404` unless WorkOS is configured and the hub host is known. Every response carries `Cache-Control: no-store`, `Referrer-Policy: no-referrer` and a `Content-Security-Policy` with no script (`default-src 'none'; style-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`). Nothing logs a code, a cookie or a query string; the routes log the sign-in id and the outcome. Anything that breaks a rule below is the fixed `400` page with nothing echoed.

| Route | Request | Response |
|---|---|---|
| `GET /login` | Optional `code`, any text up to 64 characters, shown in the form's input after normalization (uppercased, spaces and dashes removed) when it matches `^[BCDFGHJKLMNPQRSTVWXZ]{8}$`, else left empty. Touches no database, not rate limited | `200` page: "Enter the code shown in your terminal", one form `POST /login` with the `code` input and one button. The page says to continue only if the person just ran `sock login` |
| `POST /login` | Form, body at most 16 KiB: `code` as above. `Origin`, when the browser sends it, must be `https://<hub host>`, else the fixed `400`: a cross-site page cannot enter a code into someone's browser. Draws the anonymous network bucket before any lookup | A live, unapproved sign-in with that code: `Claim` with a fresh cookie, then `200` page: "A terminal, `<device label>`, wants to sign in to Sock with code `XXXX-XXXX`. Continue only if that is yours; someone may be trying to sign in to your account." One link, "Continue", to WorkOS's `/user_management/authorize` with `provider=authkit`, `redirect_uri` the terminal callback, `state` = the sign-in id and the hub's PKCE challenge for it (the OAuth consent derivation). No such sign-in, or one already approved: `400` page "That code is not valid or has expired" with nothing from the request |
| `GET /workos/terminal-callback` | WorkOS's redirect: `state` (a UUID), and either `code` (1 to 512 printable ASCII) or `error` matching `^[a-z_]{1,64}$`, never both; `error_description` ignored. Draws the anonymous network bucket | Only when the request carries `__Host-sock-terminal` whose SHA-256 equals the sign-in's claim (constant time) and the sign-in is unanswered, checked before the code is spent; without that, the fixed `400` and the cookie kept. `code`: redeemed with WorkOS (5 seconds) using the sign-in's derived verifier; `Answer` with a fresh single-use token; `200` page "Approve this terminal?" naming the terminal label, the code and who would be signed in (`@handle`, or that the address has no account yet), with one form, `POST /workos/terminal-approve`, carrying `state` and the token. `error`: `200` page "Sign-in was cancelled; run sock login again", cookie expired. WorkOS unreachable: `503` page. A refused code: `400`, cookie expired |
| `POST /workos/terminal-approve` | Form, body at most 16 KiB: `state` (a UUID) and `token` (43 base64url characters), each once. `Origin`, when sent, must be the hub. Draws the anonymous network bucket | Only when the request carries `__Host-sock-terminal` whose SHA-256 equals the sign-in's claim and the token's SHA-256 equals the answer's (both constant time), and the sign-in is unapproved: `Approve`, `200` page "Signed in. Go back to your terminal." Anything else the fixed `400`. Every answer expires the cookie: the flow ends here |

### IdentityService (new RPCs)

| RPC | Caller | Authz | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|
| `StartTerminalSignIn` | Anonymous | Sign-in bucket per client network (10 a minute); WorkOS configured and a hub host known | `device_label` 1 to 64 characters | `terminal_sign_in` (`sock_tl_…`, once), `user_code` (`XXXX-XXXX`), `verification_url` (`https://<hub host>/login`), `verification_url_complete` (with `?code=XXXX-XXXX`), `expires_at` (10 minutes), `poll_interval` (5 seconds) | `FIELD_INVALID`, `RATE_LIMITED`, `unavailable` | Not idempotent: each call opens a sign-in | `TerminalSignIn.Start`; deletes expired sign-ins first, so none is swept by a timer |
| `FinishTerminalSignIn` | Anonymous | `terminal_sign_in` parses as `sock_tl_<id>_<secret>` and names a live sign-in whose hash matches in constant time; a token naming no sign-in draws the sign-in bucket and is `TERMINAL_SIGN_IN_INVALID`; a call sooner than `poll_interval` after the sign-in's last is `RATE_LIMITED` with `Retry-After` and changes nothing | `terminal_sign_in` | Exactly one of `session`, `registration` (a `RegistrationTicket`) or `pending` (`retry_after`) | `TERMINAL_SIGN_IN_INVALID` (unknown, expired, spent, wrong secret), `RATE_LIMITED`, `NOT_REGISTERED` and `EMAIL_CODE_INVALID` (an approval by a password the hub does not trust: the user has no human, or its human never had a password sign-in; the sign-in is spent), `ACCOUNT_SUSPENDED`, `EMAIL_NOT_VERIFIED` (a ticket for an unverified address; spent), `unavailable` | Safe to repeat while pending; a success spends the sign-in and a repeat is `TERMINAL_SIGN_IN_INVALID` | `TerminalSignIn.Finish`: with an approval whose method proves the address, `Device.Issue` for the user's human or `RegistrationTicket.Issue` (as `SignInWithGoogle`); with a password approval, `SignInEmail`'s trusted-password rule; the sign-in is deleted in the same transaction |

Rate limits, added to [api](api.md): `StartTerminalSignIn` and a `FinishTerminalSignIn` whose token names no sign-in draw the anonymous sign-in bucket. A `FinishTerminalSignIn` that names a live sign-in draws no network bucket: it is paced by the sign-in's `polled_at`, one call per `poll_interval` (5 seconds). `POST /login`, `GET /workos/terminal-callback` and `POST /workos/terminal-approve` draw the anonymous network bucket, as the OAuth browser routes do, so guessing codes costs the guesser's network 10 tries a minute against a space of 20^8. `GET /login` is static.

Proto, `identity.proto`: `StartTerminalSignInRequest`, `StartTerminalSignInResponse`, `FinishTerminalSignInRequest`, `FinishTerminalSignInResponse` (a `oneof` with `TerminalSignInPending`), and `ERROR_REASON_TERMINAL_SIGN_IN_INVALID` in `errors.proto`.

## CLI

Changes to [cli](cli.md), Session:

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock login` | `StartTerminalSignIn`, then `FinishTerminalSignIn` every `poll_interval` until a result or `expires_at`; for a new person `GetRegistrationPolicy` and `RegisterWithEmail` (ticket) | `--label <text>` (default `sock CLI on <hostname>`, cut to 64), `--no-browser` (do not try to open one). Registration off a terminal: `--handle`, `--name`, `--accept-terms <version>` and, under `invite_only`, `--invite <code>`; on a terminal each is prompted for when not given, as the AuthKit spec says | To stderr first: "To sign in, open `<verification_url>` on any device and enter the code", the code alone on its own line, then "Opened your browser to that page with the code filled in" or "Open it on any device" when no browser opened or `--no-browser`; `sign-in received` when the result arrives. Then `signed in as @<handle> on device <id>`; json: `Session` with `token` cleared. `TERMINAL_SIGN_IN_INVALID` or `expires_at` passed: "sign-in timed out; run sock login again". `NOT_REGISTERED` or `EMAIL_CODE_INVALID`: "no sock account for that sign-in; sign in with Google, Apple or an emailed code to create one" |
| `sock register` | none | | "sock register is gone: sock login creates an account when you have none", exit non-zero |

The code is shown with the dash. The URL carries the code too, so a person reading the terminal can click or type either; the CLI hands the opener only a page on the hub it was configured for, never another host or scheme; the page shows the code again before AuthKit, so a person who opened the page from a message they did not expect sees what they are approving. Treat the code as you would the old URL: anyone who enters it first and signs in makes your `sock` theirs, which `signed in as @<handle>` reveals; `sock logout` then.

The config file, `keepMachineKey`, `SOCK_TOKEN` warnings and the token-exists refusal are unchanged. The `sock-client` `login` package is no longer imported by the CLI; it stays for the desktop app.

## Domain events

None new. A session is `DeviceAdded`; a registration is `HumanRegistered` and `DeviceAdded`, as the apps' email registration. `TerminalSignIn` emits nothing: its transitions are private to the sign-in and leave no trace once finished.

## Data layer

Four tables in the `identity` schema beside the auth method vocabulary. The sign-in's later facts are rows, not nullable columns: a claim, an answer and an approval each exist or do not.

```sql
CREATE TABLE identity.auth_methods (
    name text PRIMARY KEY
        -- WorkOS's authentication_method as the hub parses it:
        -- Password, MagicAuth, GoogleOAuth, AppleOAuth, Other
);
INSERT INTO identity.auth_methods VALUES ('Password'), ('MagicAuth'), ('GoogleOAuth'), ('AppleOAuth'), ('Other');

CREATE TABLE identity.terminal_sign_ins (
    id           uuid        PRIMARY KEY,
        -- made by TerminalSignIn.Start, not the database: it is in the
        -- terminal token the same call returns, as a registration ticket's is
    secret_hash  bytea       NOT NULL UNIQUE CHECK (octet_length(secret_hash) = 32),
        -- SHA-256 of the 32-byte secret shown once
    user_code    text        NOT NULL UNIQUE CHECK (user_code ~ '^[BCDFGHJKLMNPQRSTVWXZ]{8}$'),
        -- what the person types; UNIQUE among live sign-ins, Start retries
        -- on a collision. Serves POST /login's lookup
    device_label text        NOT NULL CHECK (length(device_label) BETWEEN 1 AND 64),
        -- the label Finish issues the session with
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
        -- 10 minutes after created_at; past it the sign-in is nothing
    polled_at    timestamptz NOT NULL DEFAULT now()
        -- the last FinishTerminalSignIn (Start counts as one); a call
        -- within the poll interval of it is RATE_LIMITED
);
CREATE INDEX terminal_sign_ins_expires_idx ON identity.terminal_sign_ins (expires_at);
    -- serves Start's sweep, DELETE WHERE expires_at < now()

CREATE TABLE identity.terminal_sign_in_claims (
    sign_in_id   uuid        PRIMARY KEY REFERENCES identity.terminal_sign_ins (id) ON DELETE CASCADE,
        -- PK: one browser holds a sign-in; a later claim replaces the row
    browser_hash bytea       NOT NULL CHECK (octet_length(browser_hash) = 32),
        -- SHA-256 of the __Host-sock-terminal cookie
    claimed_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE identity.terminal_sign_in_answers (
    sign_in_id         uuid        PRIMARY KEY REFERENCES identity.terminal_sign_in_claims (sign_in_id) ON DELETE CASCADE,
        -- PK: AuthKit answers once per claim; FK to the claim: no answer
        -- without a browser
    provider_user_id   text        NOT NULL CHECK (provider_user_id ~ '^user_[A-Za-z0-9]{1,64}$'),
        -- the WorkOS user AuthKit signed in. No FK: not a row here
    email_verified     boolean     NOT NULL,
    auth_method        text        NOT NULL REFERENCES identity.auth_methods (name) ON DELETE RESTRICT,
    approve_token_hash bytea       NOT NULL CHECK (octet_length(approve_token_hash) = 32),
        -- SHA-256 of the single-use token in the approve form
    answered_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE identity.terminal_sign_in_approvals (
    sign_in_id  uuid        PRIMARY KEY REFERENCES identity.terminal_sign_in_answers (sign_in_id) ON DELETE CASCADE,
        -- PK: approved once; FK to the answer: no approval without a sign-in
    approved_at timestamptz NOT NULL DEFAULT now()
);
```

A claim replacing an earlier one is a delete and an insert, so the cascade takes the earlier browser's answer and approval with it. In practice an answered sign-in refuses a new claim first.

## Threats

| Threat | Mitigation |
|---|---|
| Guessing a live user code | 20^8 codes, each live 10 minutes; `POST /login` draws the guesser's network bucket (10 a minute); a wrong code gets one fixed page |
| A person tricked into entering an attacker's code (the device flow's phishing) | The pages name the code, the terminal label (which the attacker chose, so it only catches accidents) and, after sign-in, the account, and say to approve only for a `sock login` the person just ran; the terminal prints `signed in as @<handle>`. The attacker's terminal becomes the victim's session only if the victim ignores all of it. The same residual every device flow and the loopback warning page accepted |
| An attacker who saw the code on the victim's screen enters it first | Signs the victim's terminal into the attacker's account; the terminal says so with the handle. The same residual as the old URL; the code lives 10 minutes and is spent by one finish |
| A link straight to AuthKit, skipping the hub's page | The callback requires the claim cookie; AuthKit's `state` names a sign-in that no browser without the cookie can finish |
| A cross-site page that enters an attacker's code into the victim's browser (a forced form POST), sends it to AuthKit, and lets WorkOS's redirect approve | `POST /login` and `POST /workos/terminal-approve` refuse a foreign `Origin`, and the redirect approves nothing: the person must read the approve page, which names the terminal and the account, and press Approve on the hub's own origin with the page's single-use token. The same shape as OAuth consent; RFC 8628 §5.4 |
| A callback that spends a WorkOS code without the right to finish | The claim cookie is checked before `Redeem`; a cookie-less or foreign callback spends nothing |
| A WorkOS code replayed into another sign-in | The code's PKCE challenge is derived from the sign-in id; `Redeem` with another id's verifier is refused by WorkOS |
| The terminal token on the wire or in a log | TLS; the hub logs only the sign-in id. The token spends in one `Finish` and expires in 10 minutes; its secret is hashed at rest |
| Polling as a denial of service | A live sign-in is paced per row; a dead token draws the network bucket |
| A browser finishing someone else's sign-in by changing `state` | The cookie hash must match the claim of the sign-in `state` names, in constant time |
| A password sign-in on AuthKit's page for an address nobody verified | `Finish` applies `SignInEmail`'s rule: a password is trusted only for a human with a password sign-in on record; otherwise `EMAIL_CODE_INVALID`, and no ticket is issued for a method that proves no address |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `TerminalSignIn.Start` | `StartTerminalSignIn` | none | `identity.terminal_sign_ins` insert |
| `TerminalSignIn.Claim` | `POST /login` | none | `identity.terminal_sign_in_claims` upsert |
| `TerminalSignIn.Answer` | `GET /workos/terminal-callback` | none | `identity.terminal_sign_in_answers` insert |
| `TerminalSignIn.Approve` | `POST /workos/terminal-approve` | none | `identity.terminal_sign_in_approvals` insert |
| `TerminalSignIn.Finish`, then `Device.Issue` | `FinishTerminalSignIn` | `DeviceAdded` | `identity.terminal_sign_ins` delete (cascading), `identity.devices` insert |
| `TerminalSignIn.Finish`, then `RegistrationTicket.Issue` | `FinishTerminalSignIn` | none | `identity.terminal_sign_ins` delete, `identity.registration_tickets` upsert |
| `Account.RegisterEmailHuman` from the terminal | `RegisterWithEmail` with the ticket | `HumanRegistered`, `DeviceAdded` | as the apps |
| `AuthMethod` gains `AppleOAuth` | none: parsed from WorkOS | none | `identity.auth_methods` row |

## Open questions

- Whether WorkOS reports AuthKit's Sign in with Apple as `AppleOAuth` with `email_verified` true, including for Apple's private relay addresses. Checked by the first real Apple sign-in on the canary; until then an Apple sign-in to a new address may get `EMAIL_NOT_VERIFIED`.
- Whether `sock-admin login` should move to the same flow. Its operator is matched on Apple's `sub`, which only native Sign in with Apple yields; it keeps the loopback for now.
