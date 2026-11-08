# sock CLI command contract

Pass 1. Every human RPC in [api](api.md) has a `sock` command; this file is the contract the `cli` repo builds against. Request rules, errors and authorization are the RPC's, in [api](api.md); this file holds only the command shape.

## Conventions

- Credentials, URL and precedence as today: `--url`, `--token`, `SOCK_URL`, `SOCK_TOKEN`, then `config.toml`, then `https://api.joinsock.com`. `sock login` is the only command that runs with no token; it writes one.
- Output: one line of text per item by default; with `--json`, one `protojson` object per line of the response message (or per item for lists), using protojson's default lowerCamel JSON names (the names the puppet `--json` output always used). Errors go to stderr with a non-zero exit, rewritten from `ErrorReason` into an instruction (`TOKEN_INVALID`: "run sock login"; `TERMS_NOT_ACCEPTED`: "run sock terms accept").
- An `<account>` argument is an account id, or `@handle`, which the CLI resolves with `LookupHandle` first (humans only).
- Tokens are printed only by the commands that mint them, alone on stdout, so they pipe; everything else those commands say goes to stderr. A session token is never printed: login writes it to the config file.
- Destructive commands (`erase`) ask on a terminal; off a terminal they need the confirmation flag shown.
- `login` and `register`: refuse when the config file already holds a token, and warn when `SOCK_TOKEN` is set; `--port` must be 1024 to 65535; `sock login` runs no listener: it polls the hub for 10 minutes at the interval the hub names, then gives up ([terminal sign-in](terminal-sign-in.md)). The session token goes to the config file (`0600`, directory `0700`, written by rename, `url` kept), never to stdout.
- `logout` refuses a puppet token locally, and keeps the token when the server is unreachable.
- Unary calls retry transient failures with the existing retrier; `send` keeps one idempotency key across retries.

## Session

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock login` | `StartTerminalSignIn`, then `FinishTerminalSignIn` at the interval the hub names until a result or the sign-in's expiry; for a new person `GetRegistrationPolicy` and `RegisterWithEmail` with the ticket ([terminal sign-in](terminal-sign-in.md)) | `--label <text>` (default `sock CLI on <hostname>`, cut to 64), `--no-browser` (do not try to open one). Registration off a terminal: `--handle`, `--name`, `--accept-terms <version>` and, under `invite_only`, `--invite <code>`; on a terminal each is prompted for when not given | Stderr: the page to open on any device, the code `XXXX-XXXX` on its own line, whether a browser opened, `sign-in received`. Stdout: `signed in as @<handle> on device <id>`; json: `Session` with `token` cleared. Timed out or `TERMINAL_SIGN_IN_INVALID`: "sign-in timed out; run sock login again". `NOT_REGISTERED` or `EMAIL_CODE_INVALID`: "no sock account for that sign-in; sign in with Google, Apple or an emailed code to create one" |
| `sock register` | none | | Removed: "sock register is gone: sock login creates an account when you have none", exit non-zero |
| `sock logout` | `SignOut` | none | `signed out`. Removes the token from the config file on success or `TOKEN_INVALID` |
| `sock whoami` | `GetMe` | none (exists) | Account line; for a human also `terms accepted <v>, current <v>`; json: `GetMeResponse` |
| `sock terms` | `GetMe` | none | `accepted <v>, current <v>` and the terms URL |
| `sock terms accept <version>` | `AcceptTerms` | version | `accepted terms <version>` |
| `sock subscribe` | `StartCheckout` | `--yearly` (`PRO_PLAN_YEARLY`; default `PRO_PLAN_MONTHLY`) | Opens the Checkout `url` in the browser; json: `StartCheckoutResponse` |
| `sock billing` | `OpenBillingPortal` | none | Opens the portal `url` in the browser; json: `OpenBillingPortalResponse` |

## Profile and accounts

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock name set <display name>` | `UpdateDisplayName` | display name (also for puppet tokens) | Account line |
| `sock handle set <handle>` | `ChangeHandle` | handle | Account line |
| `sock accounts get <id>...` | `GetAccounts` | 1 to 100 ids | One account line each |
| `sock accounts lookup <handle>` | `LookupHandle` | handle, with or without `@` | Account line |

## Devices and puppets

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock devices` | `ListDevices` | none | `<id>  <kind>  <label>  created <local time>` (`session` or `machine-key`), the calling device marked `(this)`; json: `Device` per line |
| `sock devices revoke <device id>` | `RevokeDevice` | a device of the caller or of a puppet the caller owns (this is also how a puppet token is revoked) | `revoked <id>` |
| `sock machine-key create` | `MintMachineKey` | `--label <text>` (default the short hostname, cut to 64) | Token on stdout, device id on stderr; json: `MintMachineKeyResponse`. Revoked with `sock devices revoke` |
| `sock puppets` | `ListPuppets` | none | Per puppet its account line, then its devices indented; json: `PuppetWithDevices` per line |
| `sock puppets create <name>` | `CreatePuppet` | name; `--token-label <text>` (required) | Token on stdout; puppet account line and device id on stderr; json: `CreatePuppetResponse` on stdout |
| `sock puppets token <puppet id>` | `MintPuppetToken` | puppet id; `--label <text>` (required) | Token on stdout, device id on stderr; json: `MintPuppetTokenResponse` |
| `sock puppets erase <puppet id>` | `ErasePuppet` | puppet id; `--yes` off a terminal | `erased <id>` |
| `sock puppets archive <puppet id>` | `ArchivePuppet` | puppet id | `archived <id>`; json: `ArchivePuppetResponse` |
| `sock puppets unarchive <puppet id>` | `UnarchivePuppet` | puppet id | `unarchived <id>` |
| `sock puppets prune` | `ListPuppets`, `ErasePuppet` | `--yes` off a terminal (on one, type `yes`); `--with-tokens` to include puppets holding live devices. Needs a session token | Erases project puppets named `<project> on <host>` that are not system or connector puppets and not listed by the config file's machine key on this server; lists candidates and erases nothing without confirmation. `erased <id>` per puppet; one failure does not stop the rest |

## Conversations and messages

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock conversations` | `ListConversations` | none (exists) | Conversation line each |
| `sock conversations direct <account>` | `CreateDirect` | an own puppet or the support puppet | Conversation line |
| `sock conversations group [<account>...]` | `CreateGroup` | 0 to 19 accounts | Conversation line |
| `sock conversations add <conversation id> <account>` | `AddMember` | | Conversation line |
| `sock conversations remove <conversation id> <account>` | `RemoveMember` | an own puppet | Conversation line |
| `sock conversations leave <conversation id>` | `Leave` | (also for puppet tokens) | `left <id>` |
| `sock send --conversation <id> [text...]` | `Send` | text or stdin (exists). A human must pass `--conversation`: without it `send` calls `NotifyOwner`, which is for puppets | Message line |
| `sock listen [--exec <cmd>]` | `Subscribe`, `Ack` | (exists) | Message lines |
| `sock fetch [--no-ack]` | `FetchPending`, `Ack` | (exists) | Message lines |
| `sock ack <message id>...` | `Ack` | 1 to 500 ids | nothing |
| `sock activity <conversation id> working\|idle` | `SetActivity` | state; also for puppet tokens, which is how a runner shows its agent working. `working` is held 30 s: call it again every 10 s | `working until <local time>` or `idle` |

## Safety and data

| Command | RPC | Args and flags | Output |
|---|---|---|---|
| `sock block <account>` | `BlockAccount` | | `blocked <id>` |
| `sock unblock <account>` | `UnblockAccount` | | `unblocked <id>` |
| `sock blocks` | `ListBlockedAccounts` | none | `<account id>  blocked <local time>`; json: `BlockedAccount` per line |
| `sock report <account>` | `FileReport` | `--reason spam\|harassment\|hate\|sexual-content\|violence\|self-harm\|impersonation\|other` (required), `--note <text>`, `--evidence <file>`: JSON lines of `Message` exactly as `sock fetch --json` or `sock listen --json` printed them, `evidence_tag` included, 0 to 20 | `filed report <id>` |
| `sock export` | `ExportIdentityData`, `ExportMessagingData` | none | Always JSON, one object `{"identity": ExportIdentityDataResponse, "messaging": ExportMessagingDataResponse}` |
| `sock account erase` | `EraseAccount` | on a terminal, type the handle to confirm; off one, `--confirm <handle>` | `erased`; removes the token from the config file |

## Not mapped

- `GetRegistrationPolicy`: read by `sock login` before it prompts for an invite; no command of its own.
- `RegisterPush`: registers an APNs token; a CLI has none.
- `OpenHistoryTransfer`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `GetHistoryTransferKey`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `ClaimHistoryTransfer`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `UploadHistoryChunk`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `ReceiveHistory`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `CloseHistoryTransfer`: the apps' phone-to-desktop history transfer; a CLI holds no history to send or import.
- `MarkRead`: reports a human's read position; the CLI has no read state, unread is an app concept.
- `NotifyOwner`: puppet only; reached by `sock send` without `--conversation`, as today.
- `OperatorService`: the operator is not a human; it has its own CLI and console.
- `SignInWithEmail`: the apps' email sign-in; `sock login` signs in through the hub's terminal sign-in instead ([terminal sign-in](terminal-sign-in.md)).
- `SignInWithApple`: the apps' native Sign in with Apple; the CLI reaches Apple through AuthKit.
- `RegisterWithApple`: the apps' native Apple registration, likewise.
- `AddEmailSignIn`: the apps' Settings, likewise.
- `SignInOperatorWithApple`: anonymous, but the console's sign-in, not a human's; the operator CLI keeps `sockd operator token mint`.

## Coverage

Human RPCs in [api](api.md), each with a command above: `StartTerminalSignIn`, `FinishTerminalSignIn`, `RegisterWithEmail`, `SignOut`, `GetMe`, `AcceptTerms`, `StartCheckout`, `OpenBillingPortal`, `GetAccounts`, `LookupHandle`, `UpdateDisplayName`, `ChangeHandle`, `ListDevices`, `RevokeDevice`, `MintMachineKey`, `CreatePuppet`, `ListPuppets`, `MintPuppetToken`, `ErasePuppet`, `ExportIdentityData`, `EraseAccount`, `CreateDirect`, `CreateGroup`, `AddMember`, `RemoveMember`, `Leave`, `ListConversations`, `Send`, `Subscribe`, `FetchPending`, `Ack`, `SetActivity`, `ExportMessagingData`, `BlockAccount`, `UnblockAccount`, `ListBlockedAccounts`, `FileReport`. Excluded with a reason: `RegisterPush`, `MarkRead`, `SignInWithEmail`, `SignInWithApple`, `RegisterWithApple`, `AddEmailSignIn`, `SignInOperatorWithApple`, `OpenHistoryTransfer`, `GetHistoryTransferKey`, `ClaimHistoryTransfer`, `UploadHistoryChunk`, `ReceiveHistory`, `CloseHistoryTransfer`.
