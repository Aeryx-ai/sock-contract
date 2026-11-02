# Context map: sock v1

Pass 11: feature flags and open registration. Pass 10: History Transfer. Pass 9: the operator CLI signs in with Apple. Pass 8: triage runs outside the hub and polls it for reports. Pass 7: agent platforms connect through OAuth. Pass 6: Sock Pro gates puppets. Pass 5: the operator console signs in with Apple. Pass 4: humans sign in from the CLI. Pass 3: the operator as a network caller; Moderation, terms, suspension, blocks and Apple grants; public network.

## Ubiquitous language

| Term | Means | Lives in |
|------|-------|----------|
| Account | A human or a puppet. Exactly one of the two | Identity |
| Human | A person, signed in with Apple, with a handle | Identity |
| Puppet | A bot or agent owned by exactly one human | Identity |
| Owner | The human a puppet belongs to | Identity, Messaging |
| Handle | A human's unique lowercase name, used to find them | Identity |
| Device (Identity) | One credential: a human's Apple sign-in session or one puppet token | Identity |
| Token | The bearer secret a caller presents: `sock_st_` (session) or `sock_pt_` (puppet) prefix and device id, or `sock_ot_` prefix and operator token id, then the secret. Shown once, stored as a hash | Identity |
| Operator | Whoever holds a live operator token. Not an account | Identity, Moderation |
| Operator token | The operator's credential: minted by the bootstrap CLI or by console sign-in, expiring, revocable | Identity |
| Console sign-in | Minting a 12-hour operator token for the configured operator Apple subject, from a verified Apple identity token whose nonce is bound to console sign-in on this hub | Identity |
| CLI sign-in | Console sign-in's twin for `sock-admin`: the same mint, with the nonce bound to the CLI and the id_token relayed to a loopback listener | Identity |
| Operator Apple subject | The Apple `sub` configured as the operator's (`SOCK_OPERATOR_APPLE_SUB`). Not an account | Identity |
| Invite mint | The fact that an operator token minted an invite over the network | Identity |
| Operator action | The record of one thing the operator did over the network, kept 365 days | Identity |
| Feature flag | One of the keys the hub declares, on or off for everyone. Not a rollout percentage and not per account | Identity |
| Flag key | The flag's name, lowercase `[a-z_]`, declared in the hub with its description | Identity |
| Flag set | The fact that an operator token set a flag to a value at a time: the `OperatorAction` with target `FlagSet` | Identity |
| Registration policy | `invite_only` or `open`: whether registering needs an invite. `open` exactly when `open_registration` is on | Identity |
| Invite | Single-use code that lets one Apple identity register | Identity |
| Participant | An account as Messaging sees it: human, or puppet with an owner | Messaging |
| Endpoint | A human's device as a place deliveries go | Messaging |
| Push registration | An endpoint's APNs address | Messaging |
| Conversation | Direct (two members, fixed) or group (1 to 20 members) | Messaging |
| Member | A participant in a conversation | Messaging |
| Message | One immutable send into a conversation, held only until delivered | Messaging |
| Seq | A message's position in its conversation, gapless from 1 | Messaging |
| Envelope | A message's stored payload: scheme plus bytes | Messaging |
| Delivery | A message waiting for one endpoint, or for one puppet | Messaging |
| Ack | A recipient's confirmation that a delivery arrived; deletes it | Messaging |
| Send receipt | The record that makes a retried send return the original result | Messaging |
| Terms version | The identifier of the published terms a human agreed to, such as `2026-10-11` | Identity |
| Terms acceptance | A human's agreement to one terms version, at a time | Identity |
| Suspension | The operator's bar on an account: no device authenticates, sign-in is refused | Identity |
| Apple grant | The refresh token Apple issues for a human's Sign in with Apple, kept only to revoke it at erasure | Identity |
| Apple client | Which Sign in with Apple client issued a credential: the app (bundle id) or the web Services ID used by `sock login` | Identity |
| Sock Pro | What lets a human own working puppets; active for a human while a trial, a Stripe subscription or an operator grant is, and for their puppets through them | Identity, Messaging (projected) |
| Trial | A human's one free 7-day Sock Pro period, no card, started the first time they pass the Pro gate | Identity |
| Subscription (billing) | A Stripe subscription bought through Checkout, as the hub last fetched it | Identity |
| Billing customer | The Stripe customer the hub created for a human, one at most | Identity |
| Grant (Pro) | Free Sock Pro the operator gave a human until a date | Identity |
| Active puppet | A puppet that lets humans post: a system puppet, or one not archived, neither it nor its owner suspended, whose owner has Sock Pro | Messaging |
| Read-only conversation | A conversation with no active puppet: listed, readable, humans' posts refused | Messaging |
| Evidence tag | The server's HMAC over a delivered message's identity and text hash, which makes report evidence verifiable | Messaging, Moderation |
| OAuth client | An agent platform that asks to act in sock: known by its client metadata document (ChatGPT, Claude) or by dynamic registration, with exact redirect URIs | Identity |
| Consent | A human's Allow on the page that names an OAuth client and the puppet it will be: the third web Sign in with Apple flow | Identity |
| Connector grant | The result of one consent: one OAuth client acting for one human as one puppet, with its codes and refresh tokens; revoked as a whole | Identity |
| Connector puppet | The puppet a connector grant owns, created at consent and erased at revocation; an ordinary puppet to Messaging | Identity, Messaging |
| Access token | A one-hour signed JWT a platform presents to `sock-mcpd`; audience the resource server, subject the grant. Useless at the hub | Identity, Connector |
| Refresh token | `sock_rt_` token a platform trades for the next access token; rotated on every use | Identity |
| Connector service token | `sock_ct_` credential of `sock-mcpd` itself, good only for `IssueConnectorPuppetToken` and `RevokeConnectorDevice` | Identity |
| Connection | `sock-mcpd`'s view of one grant: its puppet token and its platform's subscriptions | Connector |
| Event subscription | A platform's webhook for one event on one connection, with its secret and cursor | Connector |
| Owner reply event | A delivery from the grant's owner, forwarded to the platform as `owner.reply`; anything from another sender is acked and dropped | Connector |
| Delivery stream | Connector's word for Messaging's `Subscribe` on the connector puppet, to keep it apart from a platform's subscription | Connector |
| Block | A human's choice that an account's messages never reach them | Messaging |
| Report | A human's complaint to the operator about an account | Moderation |
| Reporter | The human who filed a report | Moderation |
| Subject | The account a report is about | Moderation |
| Evidence | Messages from the reporter's own history attached to a report, stored as the reporter gave them | Moderation |
| Reason | Why a report was filed, from a closed list | Moderation |
| Resolution | The operator's decision on a report: dismissed, or subject suspended | Moderation |
| Moderation puppet | A puppet the operator owns, through which report alerts reach the operator | Moderation |
| History transfer | One move of a phone's local history to one desktop of the same human, from the desktop opening it to its deletion | History Transfer |
| Transfer secret | 32 random bytes the desktop shows in a QR code; proves a caller saw it. Never stored by the hub | History Transfer |
| Binding tag | `HMAC-SHA-256` of the transfer secret and the human's account id, computed by the hub; its only handle on a transfer | History Transfer |
| Recipient key | The desktop's one-time HPKE public key; the private half never leaves the desktop's memory | History Transfer |
| Claim | The phone's acceptance of a transfer, carrying its HPKE encapsulated key | History Transfer |
| Chunk | One HPKE-sealed slice of the archive, at most 1 MiB of plaintext | History Transfer |
| Seal | The fact that the final chunk is stored: the transfer is complete | History Transfer |
| Archive | The plaintext the chunks carry: a stream of length-delimited `HistoryRecord` protobufs | History Transfer |
| Case | Triage's unit of work: one feedback conversation or one report, with its lifecycle. Model lives in `sock-triage` | Triage |

## Contexts

| Context | Subdomain | About |
|---------|-----------|-------|
| Identity | supporting | Who an account is, how it proves it, who owns which puppet |
| Messaging | core | Conversations and store-and-forward delivery between humans and puppets, and who has blocked whom |
| History Transfer | supporting | Moving a phone's history to a desktop once, as ciphertext the hub holds for at most an hour. Shares no data with Messaging or Identity |
| Moderation | supporting | Reports, their evidence, and the operator's resolutions |
| Client (Apple app) | supporting | Local history and presentation on iPhone and Mac. Model lives in the `apple` repo |
| Triage | supporting | The operator's case queue over feedback and reports, worked by agents. Service `sock-triage`, its own Postgres; model in its own repo |
| Connector | supporting | An OAuth-connected agent platform talking to sock over MCP: connections, event subscriptions, the owner reply event. Service `sock-mcpd`; model lives in `sock-integrations` |

OAuth inside Identity is generic: the model conforms to the RFCs and stays shallow (client, pending request, grant, code, refresh token).

Apple (Sign in with Apple, APNs) and Stripe (Checkout with Managed Payments, the Customer Portal, webhooks) are generic and sit behind adapters, not in a context.

## Relationships

| Upstream | Downstream | Pattern | Notes |
|----------|-----------|---------|-------|
| Identity | Messaging | Customer/Supplier, Published Language | Messaging keeps its own participant and endpoint rows, built from Identity's published events ([events](events.md)) |
| Identity, Messaging, Moderation | Client, CLI, puppets, operator console | Open Host Service, Published Language | The Connect API in `proto/`. The `sock` CLI is a human device as well as a puppet tool ([cli](cli.md)). The operator console (`sock-console`) is a browser caller of `OperatorService` and `OperatorSignInService` only, reached through CORS; `sock-admin` calls the same two from a terminal ([admin-cli](admin-cli.md)) |
| Sign in with Apple | Identity | ACL | `server/internal/apple`; Apple JWT claims never leave it |
| Stripe | Identity | ACL | `server/internal/stripe` behind the `Billing` interface ([domain model](domain-model.md)); no `stripe-go` type leaves it, only customer and subscription ids, a plan, a subscription snapshot and a verified event's kind |
| APNs | Messaging | ACL | `server/internal/apple`; `apns2` types never leave it |
| Identity | History Transfer | Conformist | Authentication only: the interceptor yields the caller's human, from which the hub computes the binding tag. History Transfer stores no account or device id |
| Messaging | History Transfer | Separate Ways | No shared data; a message here is an opaque byte stream the hub cannot read |
| Messaging | Identity | Separate Ways | Identity never reads Messaging data |
| Identity | Moderation | Customer/Supplier | Moderation asks Identity whether an account exists, is human, and its handle (in-process query port); consumes `AccountErased` |
| Messaging | Moderation | Customer/Supplier | `ModerationAlert` posts through Messaging as the moderation puppet |
| Moderation | Identity | Customer/Supplier, Published Language | Identity consumes `ReportResolved` and suspends the subject |
| `sock-mcp`, `sock` CLI | Integrations | Conformist | `sock-integrations` packages them per agent platform (Claude Code plugin, Codex config, Agent SDK example, the headless runner and the Managed Agents bridge). No domain model and no contracts of its own |
| Identity | Connector | Open Host Service, Published Language | OAuth 2.1 (RFC 8414 metadata, authorization code with PKCE, CIMD and DCR, refresh rotation) and `IssueConnectorPuppetToken`. Connector holds grant ids and puppet tokens, never a human identity |
| Messaging | Connector | Conformist | `sock-mcpd` is an ordinary puppet caller per grant: `Subscribe`, `Ack`, `NotifyOwner`, `Send`, `ListConversations` |
| Moderation | Triage | Open Host Service, Published Language; Triage conformist | `ListReports`, polled, and `ResolveReport` with an operator token. Triage holds report and account ids and its own copy of feedback text, never the hub's database |
| Messaging | Triage | Conformist | Feedback arrives as deliveries to the support puppet over `Subscribe`, acked once the case entry commits; replies are `Send` as the support puppet |
| Connector | ChatGPT, dots, claude.ai, Claude Code | Open Host Service, Published Language | MCP over Streamable HTTP at `mcp.joinsock.com`, protected by the hub's tokens; `owner.reply` events by webhook to platforms that subscribe. Model and spec in `sock-integrations` (`docs/specs/connector.md`) |

## Ambiguous terms

| Term | Identity meaning | Messaging meaning | Resolution |
|------|------------------|-------------------|------------|
| device | A credential: what proves who is calling | Called endpoint: where deliveries go | Same id, separate types. Messaging calls it `Endpoint` |
| account | The full record: handle, Apple subject, display name | Called participant: id, and owner if a puppet | Messaging calls it `Participant` and holds no names |
| subscription | A Stripe subscription to Sock Pro | A puppet's `Subscribe` delivery stream | Identity says `ProSubscription`. Connector has a third meaning, a platform's webhook registration for an event: it says `event subscription` for its own and `delivery stream` for Messaging's |

## Stored and derived

Stored:
- Identity: accounts, the human or puppet half of each, devices with token hashes, unredeemed invites.
- Identity also: terms acceptances, suspensions, Apple grants, grants awaiting revocation, sign-in buckets per client network, operator tokens with token hashes, operator token revocations, operator token sign-ins, invite mints, operator actions and their targets, Sock Pro trials and their reminder rows, billing customers, Stripe subscriptions as last fetched, billing customers awaiting deletion, Pro grants and their revocations. Never an email address or card data for billing.
- Messaging: participants and puppet owners (copied from Identity events), endpoints, push registrations, conversations with next seq, memberships, undelivered messages, deliveries, send receipts for 24h, blocks and the projected Pro entitlement, suspension and puppet archive of each account.
- History Transfer (schema `transfer`): per live transfer the binding tag, recipient key, encapsulated key, chunk ciphertexts, chunk count at seal and `opened_at`. Nothing after `Close` or the 60-minute deadline; no account, device or secret.
- Moderation: reports with their evidence, resolutions, report buckets per reporter.
- Identity also: OAuth clients and their redirect URIs, pending authorization requests with their raw Apple nonce, connector grants, authorization code hashes, refresh token hashes and rotations, grant revocations, the connector device per grant, connector service token hashes.
- Triage (`sock-triage`, its own database): cases, their entries (feedback text byte-exact, purged 180 days after resolution or at erasure), classification, beads links.
- Connector (`sock-mcpd`, its own database): per grant the encrypted puppet token, event subscriptions with webhook URL, secret ciphertext and cursor, subscription closures. Never a message body.

Derived, never stored:
- Connector grant live: no revocation row and the puppet exists.
- Access tokens: signed, never stored; the JWKS verifies them.
- Account kind: from which half exists.
- Token prefix: from account kind.
- Participant kind: human unless a puppet owner row exists.
- Delivered: the delivery row no longer exists.
- Message still held: at least one delivery exists.
- Whether a device has a live stream: replica memory only, never persisted.
- Terms current: an acceptance row exists at the configured version.
- Suspended: a suspension row exists.
- Sock Pro active: `ProActiveUntil()`, the latest end of the trial, subscriptions and unrevoked grants, is after now.
- Active puppet and read-only conversation: computed at post and at `ListConversations` from Messaging's projections.
- Operator token live: no revocation row exists and `expiresAt` is in the future.
- Report open: no resolution row exists.
- History transfer state: from which rows exist (opened, claimed, sealed); its deadlines from `opened_at`; its owner is never stored, recomputed per call.
- Evidence tag: computed from the message and the server key whenever a message is returned or delivered, never stored by the server.

## Still open

- Sign in with Apple on the web for anything beyond `sock login`, console sign-in and CLI sign-in (each flow gets its own nonce binding, and its own return URL unless it shares an existing one's target and `state` grammar, as CLI sign-in does).
- The proxy header that carries the client address on Render.
- Whether a suspended account may still export its data (v1: no device authenticates, so export is by request to support).
- Whether account profiles stay visible to every signed-in account at public launch (v1: yes).
- OAuth signing key rotation: two keys in the JWKS during a changeover, and how long the old one verifies.
- Whether consent offers to reuse an existing grant for the same client (today every consent creates a new puppet).
- A credential scoped to creating and erasing puppets, for agent hosts that mint one puppet per project (machine keys pending the server team's answer on shape and the 20-puppet cap).
