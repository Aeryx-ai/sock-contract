# CLI sign-in through AuthKit

Draft, pass 1. Contexts: Identity. Builds on [Google and passkey sign-in on desktop](google-and-passkey-sign-in-on-desktop.md) (the hub's WorkOS relay and the loopback listener), [email sign-in, pass 2](email-sign-in-pass-2.md) (`SignInWithEmail` with an AuthKit code), [pass 3](email-sign-in-pass-3.md) (the registration ticket) and [feature flags](feature-flags.md) (the registration policy). All stay in force except where this spec says otherwise.

What changes:

- `sock login` opens AuthKit's hosted page in the browser, where WorkOS offers Apple, Google and email (code or password), and takes the result on its loopback listener. The CLI no longer runs Sign in with Apple itself.
- One command signs in and registers. The hub says whether the address belongs to a human; a new person is asked for a handle, a name and the terms in the terminal, and for an invite only while registration is invite-only.
- `sock register` is removed.
- Apple through WorkOS is a new email identity, not a match for a human who registered with Apple in the apps or with `sock register`. The operator's account already has email sign-in; before release, the builder confirms in production that every human holding a CLI device (`APPLE_CLIENT_WEB`) holds an `email_identities` row, and a human who does not adds email sign-in from the app's Settings. Native Apple leaves the CLI in this release; the apps keep it until their own pass.
- `sock-admin login` is unchanged: the operator is matched on Apple's subject, which only native Sign in with Apple yields.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| AuthKit relay | The hub's warning page and callback carrying a WorkOS code from AuthKit's hosted page to the loopback listener: the desktop Google relay with `provider=authkit` | Not stored |
| CLI sign-in (changed) | `sock login`: the AuthKit relay, then `SignInWithEmail` with the code and the CLI's PKCE verifier, then registration with the ticket when the address has no human | Not stored: the session or the ticket is the trace |
| Apple through WorkOS | AuthKit's Sign in with Apple, a WorkOS user like any email identity. Proves the address, as Google does | Stored (`identity.email_identities`) |

## Domain model

Changes to [domain-model](domain-model.md). Unchanged objects are not repeated.

### Identity

#### AuthMethod (changed)

Enumeration, from WorkOS's `authentication_method`: `Password`, `MagicAuth`, `GoogleOAuth`, `AppleOAuth` (new) and `Other`. `ProvesAddress` is true for `MagicAuth`, `GoogleOAuth` and `AppleOAuth`.

#### RegistrationTicket (changed)

Issued also by `SignInWithEmail` when the redeemed WorkOS user holds no human and the caller asked for one. Unchanged otherwise.

#### Account

No change: `RegisterEmailHuman` with a ticket, as the apps do.

## Configuration

| Variable or setting | Meaning |
|---|---|
| WorkOS dashboard, per environment | Sign in with Apple enabled on the AuthKit connection, with its own Apple Services ID and key registered at Apple for WorkOS's return URL. Magic Auth, Password and Google as today. AuthKit's hosted page shows every enabled method |
| `SOCK_WORKOS_DESKTOP_REDIRECT_URI` | Unchanged and reused: WorkOS returns every relay to it |

No new hub variable: the relay is on wherever the desktop Google relay is. Release prerequisite, in staging and production: `SOCK_WORKOS_DESKTOP_REDIRECT_URI` set in the Render dashboard (`render.yaml` declares it `sync: false`, so the repo cannot show it) and the URI registered in that environment's WorkOS dashboard. Check: `GET /workos/desktop-login` with no query answers `400`, not `404`. Unset, `sock login` has no sign-in path once native Apple is gone.

## Caller classes

Unchanged. The relay is anonymous and touches no database; `sock login` is anonymous until it holds a session.

## API

### Hub routes (changed, [api](api.md), Operational)

`GET /workos/desktop-login` gains `provider`: `google` or `authkit`; absent is `google`, so deployed desktop builds are unchanged; any other value breaks the rules (fixed `400`). With `authkit` the link is WorkOS's `/user_management/authorize` with `provider=authkit` and the same `redirect_uri`, `state`, `code_challenge` and `code_challenge_method`, and the page reads: "Continue only if you just started signing in from the Sock app or `sock login` on this computer. Someone may be trying to sign in to your account." with one link, "Continue". Cookie, headers and refusals are unchanged. `GET /workos/desktop-callback` is unchanged: it relays to `http://127.0.0.1:<port>/google` whatever the provider, since it cannot tell and need not.

### IdentityService (changed)

| RPC | Caller | Authz | Request | Response | Errors | Idempotency | Domain behavior |
|---|---|---|---|---|---|---|---|
| `SignInWithEmail` (changed) | Anonymous | As today, for any `AuthMethod`: the code is AuthKit's, whichever method the person chose on its page | As today, plus `accept_registration_ticket` (bool, default false) | `session` as today; or, only when `accept_registration_ticket` and the WorkOS user holds no human, `registration` (a `RegistrationTicket`), exactly one of the two set | As today; `NOT_REGISTERED` only when `accept_registration_ticket` is false, so released app builds see what they always saw | Not idempotent: each call issues a device or a ticket; the code is single use at WorkOS | `Device.Issue` as today; or `RegistrationTicket.Issue` (pass 3), which refuses an unverified address (`EMAIL_NOT_VERIFIED`) |
| `RegisterWithEmail` | Anonymous | Unchanged: ticket or AuthKit code, Admission as [feature flags](feature-flags.md) | unchanged | unchanged | unchanged | unchanged | unchanged |

Proto, `identity.proto`:

```proto
message SignInWithEmailRequest {
  string code = 1;
  string code_verifier = 2;
  string device_label = 3;
  // True: a WorkOS user with no human gets `registration` instead of
  // NOT_REGISTERED. Released app builds send false.
  bool accept_registration_ticket = 4;
}
message SignInWithEmailResponse {
  // Exactly one of the two is set.
  Session session = 1;
  RegistrationTicket registration = 2;
}
```

A sibling field, not a `oneof`: moving `session` into one is a breaking change under `buf breaking` `FILE`.

### Loopback listener (sock-client `login`)

The `login` package gains `AuthKit(ctx, AuthKitOptions)`, `Google` with the warning page's URL carrying `provider=authkit`. Same listener rules, same `/google` route (the hub's callback relays every WorkOS code there), same `GoogleResult` shape renamed `WorkOSResult`: the code and the verifier. `Google` stays for the desktop app.

## CLI

Changes to [cli](cli.md), Session:

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock login` | `SignInWithEmail` (`accept_registration_ticket`), then for a new person `GetRegistrationPolicy` and `RegisterWithEmail` (ticket) | `--label <text>`, `--no-browser`, `--port <n>` as today. For registration off a terminal: `--handle <handle>`, `--name <display name>`, `--accept-terms <version>` and, under `invite_only`, `--invite <code>`; on a terminal each is prompted for when not given | `signed in as @<handle> on device <id>`; json: `Session` with `token` cleared. A new person first sees "No sock account for this address yet. Let's make one." |

Registration on a terminal, in order: handle (checked locally against the handle rule before sending), display name, invite code (only under `invite_only`, with "Registration needs an invite; request one at https://joinsock.com/invite/"), then the terms: the CLI prints the terms URL, the privacy URL and the version this build ships, and asks to type `yes`. Anything else cancels with "not registered"; the ticket expires on its own. Off a terminal with a flag missing: "run sock login on a terminal, or pass --handle, --name and --accept-terms" (and `--invite` when required), exit non-zero, nothing sent.

`HANDLE_TAKEN`, `INVITE_INVALID` and `TERMS_NOT_ACCEPTED` re-prompt once on a terminal; off one they are errors. A ticket that expired (`EMAIL_CODE_INVALID` from `RegisterWithEmail`) says to run `sock login` again.

`sock register` is removed; running it says "sock register is gone: sock login creates an account when you have none". `--invite` on `sock login` without a registration is ignored with a warning.

The config file, `keepMachineKey`, `SOCK_TOKEN` warnings and the token-exists refusal are unchanged.

Not mapped, added: `SignInWithApple` and `RegisterWithApple` (the apps' native Apple sign-in; the CLI signs in through AuthKit, where Apple is one of WorkOS's methods). Coverage: `SignInWithEmail`, `RegisterWithEmail` and `GetRegistrationPolicy` move from excluded to mapped.

## Domain events

None new. A session is `DeviceAdded`; a registration is `HumanRegistered` and `DeviceAdded`, as the apps' email registration.

## Data layer

None. `AppleOAuth` is a value the hub parses, not a column: `email_identities` stores the WorkOS user id whatever the method.

## Threats

| Threat | Mitigation |
|---|---|
| An AuthKit code read on the loopback hop | Useless without the PKCE verifier, which never leaves the CLI process (ADR 44) |
| A registration ticket leaking | Returned in the RPC response to the CLI process only, never on the loopback or in a URL; 15 minutes, hashed at rest, spent by registration (pass 3) |
| `provider` reflected into the page | A closed set of two values, chosen by a template branch; nothing from the query reaches the page but through WorkOS's URL, as today |
| A hostile local listener that starts its own AuthKit flow | The residual ADR 44 accepts, behind the same warning page |
| A password sign-up on AuthKit registering with an unverified address | `RegistrationTicket.Issue` refuses an unverified address; `ProvesAddress` excludes `Password` |
| Apple through WorkOS impersonating the operator | The operator is matched on Apple's subject in `SignInOperatorWithApple` only; an `AppleOAuth` email identity reaches no operator path |
| A deployed app build seeing `registration` where it expects `NOT_REGISTERED` | Gated by `accept_registration_ticket`, which deployed builds never send |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `Device.Issue` from the CLI | `SignInWithEmail` through the AuthKit relay | `DeviceAdded` | as the apps |
| `RegistrationTicket.Issue` from the CLI | `SignInWithEmail` with `accept_registration_ticket` | none | `registration_tickets` as pass 3 |
| `Account.RegisterEmailHuman` from the CLI | `RegisterWithEmail` with the ticket | `HumanRegistered`, `DeviceAdded` | as the apps |
| `AuthMethod` gains `AppleOAuth` | none: parsed from WorkOS | none | none |

## Open questions

- Whether WorkOS reports Apple sign-ins as `AppleOAuth` in `authentication_method` and marks the address verified, including Apple's private relay addresses. Confirm against a real sign-in on staging before `ProvesAddress` trusts it.
- Whether the existing Apple Services ID can carry WorkOS's return URL beside the hub's, or WorkOS needs a second one. Apple allows several return URLs per Services ID; the dashboard decides.
