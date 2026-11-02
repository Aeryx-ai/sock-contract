# sock-admin command contract

Pass 1. `sock-admin` is the operator console's work from a terminal: every RPC the console calls has a command, and nothing else does. This file is the contract `sock-admin` builds against. Request rules, errors and authorization are the RPC's, in [api](api.md); this file holds the command shape, the sign-in flow and the contract changes sign-in needs.

## Boundaries

A downstream of the Open Host Service, conforming to the Published Language in `proto/sock/v1`, like the console: it has no domain model of its own, decides nothing the hub does not, and owns no events or tables. It calls `OperatorSignInService` anonymously and `OperatorService` with the operator token, and no other service. It never reads `sock`'s config file or `SOCK_TOKEN`.

Out of scope until the console has them: the Users RPCs (`ListHumans`, `GetHuman`, `ListAccountReports`, `EraseHuman`, `GrantPro`, `RevokeProGrant`).

## Conventions

- Binary `sock-admin`, Go, cobra, `go-sdk`'s client (`Client.Operator`) and retrier.
- Config file `$XDG_CONFIG_HOME/sock-admin/config.toml`, else `~/.config/sock-admin/config.toml`: `url`, `access_gate`, `token`, `expires_at` (RFC 3339). File `0600`, directory `0700`, written by rename; `sock-admin` refuses to run when a file holding `token` or `access_gate` is readable by group or others. A missing file is fine.
- Server: `--url`, then `SOCK_ADMIN_URL`, then the file's `url`, then `https://api.joinsock.com`. Access gate: `SOCK_ACCESS_GATE`, then the file's `access_gate`, sent as `Sock-Access-Gate` to the configured server only, never printed.
- Token: `SOCK_ADMIN_TOKEN` (break-glass, a token from `sockd operator token mint`), then the file's `token`. A token from the file goes only to the file's server: a `--url` or `SOCK_ADMIN_URL` naming another is refused, as `sock` does. There is no `--token` flag, since a flag shows in `ps`. A token without the `sock_ot_` prefix is refused locally.
- A file token past its `expires_at` is removed before any call, and the command says `signed out: the session expired; run sock-admin login`.
- Output: one line of text per item by default; with `--json`, one `protojson` object per line of the response message (or per item for lists), default lowerCamel names. Errors go to stderr with a non-zero exit, rewritten from `ErrorReason`: `CALLER_NOT_ALLOWED` on an `OperatorService` call becomes "the operator token was refused (expired, revoked or read scope); run sock-admin login", and removes a file token.
- Codes and tokens are printed only by the commands that mint them, alone on stdout, so they pipe; everything else those commands say goes to stderr. A sign-in token is never printed: `login` writes it to the config file.
- Unary calls retry transient failures with `go-sdk`'s retrier. `MintInvite` is not idempotent and is never retried after the request may have reached the hub.
- `<account>` is an account id; `sock-admin` has no handle lookup (the operator token reaches no `IdentityService` RPC).

## Sign-in

1. `login` refuses when the config file holds a token, and warns when `SOCK_ADMIN_TOKEN` is set (it overrides the saved token).
2. It runs `sock-client/login` against the server: a loopback listener on `127.0.0.1`, `state` = `<port>.<43 base64url>`, a 43-character base64url raw nonce, and `nonce` on `/apple/cli-login` = lowercase hex SHA-256 of `sock-operator-cli-signin <hub host> <raw nonce>` (UTF-8, single spaces), where the hub host is the host of the configured server URL. The listener gives up after 10 minutes and ignores callbacks with the wrong `state` or `Host`.
3. The hub relays Apple's post to the listener unchanged ([api](api.md), Operational). `sock-admin` drops the `code`.
4. It calls `SignInOperatorWithApple` with no `Authorization` header, `identity_token`, the raw `nonce` and `client = CLI`, and writes `token`, `expires_at` and `url` to the config file. A token that arrived in the file during the browser wait is kept, and the new one is revoked with `RevokeOperatorToken` before exiting with an error.

`--no-browser` prints the login URL instead of opening it, and `--port <n>` (1024 to 65535) fixes the listener's port for SSH forwarding, as `sock login`'s do.

## Commands

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock-admin login` | `SignInOperatorWithApple` (`client = CLI`) | `--no-browser`, `--port <n>` | `signed in; the session expires <local time>`. `CALLER_NOT_ALLOWED`: "this Apple ID is not the operator, or operator sign-in is off on this server" |
| `sock-admin logout` | `RevokeOperatorToken` | none | `signed out`. Removes the file token on success or `CALLER_NOT_ALLOWED`; keeps it when the server is unreachable. With only `SOCK_ADMIN_TOKEN`, revokes that token and says to unset it |
| `sock-admin invites mint` | `MintInvite` | none | The code alone on stdout; `expires <local time>` on stderr; json: `MintInviteResponse` |
| `sock-admin reports list` | `ListReports` | `--limit <1..25>`, `--after <report id>` | Each open report, oldest first: id, reason, created, reporter and subject as `@handle (id)` (puppets with `owner @handle`), the note, and each evidence message's sender and text, with control characters escaped. `next: --after <id>` on stderr when the page is full. json: `ListReportsResponse` |
| `sock-admin reports resolve <report id> <outcome>` | `ResolveReport` | `dismiss`, `suspend` or `suspend-owner` (`DISMISSED`, `SUBJECT_SUSPENDED`, `OWNER_SUSPENDED`), as `sockd reports resolve` | `<report id> <outcome> at <local time>`; json: `Resolution` |
| `sock-admin accounts reinstate <account id>` | `ReinstateAccount` | account id | `reinstated <account id>` |
| `sock-admin requests list` | `ListInviteRequests` | `--state pending\|approved\|declined` (default every state), `--limit <1..100>`, `--after <request id>` | `<id>  <state>  <email>  requested <local time>` per request, newest first. `next: --after <id>` on stderr when the page is full. json: `InviteRequest` per line |
| `sock-admin requests approve <request id>...` | `ApproveInviteRequests` | 1 to 100 distinct ids | `<id>  <outcome>` per id, in order (`approved`, `approved, no code`, `declined`, `not found`). Exit non-zero when any outcome is not `approved`. json: `ApproveInviteRequestResult` per line |
| `sock-admin requests decline <request id>...` | `DeclineInviteRequests` | 1 to 100 distinct ids | As `approve`, with the decline outcomes |
| `sock-admin flags list` | `GetFeatureFlags` | none | `<key>  on|off  <description>` per flag, then `  set by <label or id> at <local time>` when set; json: `FeatureFlag` per line |
| `sock-admin flags set <key> on\|off` | `SetFeatureFlag` | key, value | `<key> on|off`; json: `FeatureFlag`. Asks for confirmation on a terminal; off one, `--yes` |

`resolve` with `suspend` or `suspend-owner`, `requests decline` and `flags set`, ask for confirmation on a terminal; off a terminal they need `--yes`.

## Contract changes

- `SignInOperatorWithAppleRequest` gains `OperatorSignInClient client = 3`: `OPERATOR_SIGN_IN_CLIENT_UNSPECIFIED` (the console, so the deployed console is unchanged), `OPERATOR_SIGN_IN_CLIENT_CONSOLE`, `OPERATOR_SIGN_IN_CLIENT_CLI`. An unknown value is `FIELD_INVALID` (`client`). Nothing else in `operator.proto` changes.
- `SignInOperatorWithApple` verifies the nonce claim against the named client's binding: `sock-operator-signin <host> <nonce>` for the console, `sock-operator-cli-signin <host> <nonce>` for the CLI. The wrong binding is `APPLE_TOKEN_INVALID`, as any nonce mismatch is. Success logs the client beside the token id.
- `OperatorToken.SignIn(appleIdentity, operatorSubject, client)` labels the token `console sign-in` or `sock-admin sign-in`. Lifetime, scope, `OperatorTokenSignIn` and `OperatorAction(signInWithApple)` are unchanged.
- `SignInWithApple` and `RegisterWithApple` also refuse a raw nonce beginning with `sock-operator-cli-signin ` (`FIELD_INVALID`, `nonce`).
- `GET /apple/cli-login`'s page tells the person to continue only if they just started signing in from the sock app, `sock login` or `sock-admin login` on this computer.
- `sock-client/login` gains `Options.Claim func(raw string) string`, the string whose SHA-256 goes to Apple; nil means the raw nonce itself, so `sock login` and the desktop app are unchanged.

No events: nothing reacts to a sign-in, as for the console. No DDL: the client is recorded only in the token's label. No rule reads it, and `sockd operator token list` and the audit trail show the label, which is all the distinction is for. A column would earn its place only when a rule depends on the client.
