# Web inbox, pass 2: Sock Pro

Pass 2, approved. Pass 1 ([web inbox](web-inbox.md)) was approved and stays in force except where this spec says otherwise. Contexts: Identity (the session, the billing customer), Billing through the Stripe adapter ([Sock Pro billing](sock-pro-billing.md)). ADR 60.

A signed-in human subscribes to Sock Pro, or opens Stripe's Customer Portal, from the inbox page. Checkout and the portal return the browser to `/inbox`.

Why: nothing calls `StartCheckout` today. The CLI has no `sock subscribe`, the apps hide Pro (`SockPro.offered = false`), and joinsock.com has no `/pro` page for Checkout to return to. Stripe has never seen a subscription. The inbox is the one signed-in web page the hub already serves.

Pass 1's non-goal "billing" is lifted. Its other non-goals stand.

## What changes

- The Inbox browser class reaches two more routes: `POST /inbox/subscribe` and `POST /inbox/billing`. It still writes nothing to Messaging.
- The inbox page gains a Sock Pro section.
- `StartCheckout`'s and `OpenBillingPortal`'s bodies become two application services, `StartCheckout(humanId, plan)` and `OpenBillingPortal(humanId)`, in `internal/api` beside both callers. The RPCs and the inbox routes call the same functions; no rule is copied.
- `SOCK_BILLING_RETURN_URL` is the hub's `/inbox` in every environment, and the Customer Portal's `default_return_url` with it.

## Domain model

No aggregate, invariant or event changes. `Account.BindBillingCustomer` is invoked from one more route through the same application service.

### Application services (moved, not new)

- `StartCheckout(humanId, plan) (url, error)`: `ProSources`; `ErrAlreadySubscribed` when a subscription keeps Pro active; the human's `BillingCustomer`, else `Billing.CreateCustomer` then `Account.BindBillingCustomer`; then `Billing.CheckoutURL`. A Stripe failure is `ErrBillingUnavailable`. Unchanged from the RPC's body today.
- `OpenBillingPortal(humanId) (url, error)`: the human's `BillingCustomer`, else `ErrNoSubscription`; then `Billing.PortalURL`. Unchanged.

The terms gate stays outside them, where each caller already enforces it: the interceptor for the RPCs, the subscribe route for the inbox (below).

## Configuration

| Variable or setting | Meaning |
|---|---|
| `SOCK_BILLING_RETURN_URL` | `https://api.joinsock.com/inbox` on production, `https://staging-api.joinsock.com/inbox` on staging. Production's change ships with the next release |
| Customer Portal `default_return_url`, per Stripe environment | The same URL: `stripe-setup.sh` rerun with `RETURN_URL` set to it |
| `deploy/cloud/stripe-setup.sh` | Default `RETURN_URL` becomes the hub's `/inbox` for `HUB_HOST` |

## Caller classes

Changes to pass 1's Inbox browser class:

| Class | Authenticates with | Trusted to assert | Never trusted to assert |
|---|---|---|---|
| Inbox browser | As pass 1. Reaches `GET /inbox`, `POST /inbox/sign-out`, `POST /inbox/subscribe` and `POST /inbox/billing` | As pass 1, and that the human wants Checkout or the portal | As pass 1; which Stripe customer is theirs (only `identity.billing_customers` says so) |

## API

### Hub routes (new)

Pass 1's headers, logging and fixed `400` apply. Each route requires `Origin`, when sent, to be `https://<hub host>`, a body of at most 16 KiB, and a cookie naming a live session; without a live session, `303` to `/inbox` (the signed-out page). A suspended or archived human: as pass 1's `GET /inbox`. Each draws the session's inbox bucket; over it, the `429` page. Billing off (no Stripe keys): both routes `404`, and the page shows no Pro section.

| Route | Request | Response | Invokes |
|---|---|---|---|
| `POST /inbox/subscribe` | Form field `plan`, exactly once, `monthly` or `yearly` | Current terms not accepted: `200` page "Accept the current terms in a Sock app, then come back". `ErrAlreadySubscribed`: `303` to `/inbox`, which shows the subscription. Else `303` to the Checkout URL. Stripe unreachable: pass 1's `503` page. Another `plan`: the fixed `400` | `StartCheckout(session.humanId, plan)` |
| `POST /inbox/billing` | No fields | `ErrNoSubscription`: `303` to `/inbox`. Else `303` to the portal URL. Stripe unreachable: `503` page | `OpenBillingPortal(session.humanId)` |

Exempt from terms, as `OpenBillingPortal` is: `POST /inbox/billing`. Subject to terms, as `StartCheckout` is: `POST /inbox/subscribe`.

Idempotency: as the RPCs. A retried subscribe makes one customer (Stripe idempotency key) and a new Checkout Session; unused sessions expire.

### CSP

`form-action` on the signed-in inbox page and the two routes admits `'self'` and the Stripe hosts Checkout and the portal answer with, since browsers apply `form-action` to a form's redirects: `https://checkout.stripe.com` and `https://billing.stripe.com`, read from a staging Managed Payments Checkout Session and portal session on 2026-11-20. They are constants in `internal/api`; the fake Stripe answers on the same hosts, and a black-box test checks each redirect's host is in the page's `form-action`. Signed-out pages keep `form-action 'self'`.

### The inbox page (changed)

A Sock Pro section, after the header, from `ProSources.Status()` and whether a `BillingCustomer` exists:

- No Pro source: "Sock Pro is $14.99 a month or $149.99 a year." Two forms, "Subscribe monthly" and "Subscribe yearly", each posting `plan` to `/inbox/subscribe`.
- A subscription: "Sock Pro, renews or ends {date}." and a "Manage billing" form posting to `/inbox/billing`.
- A grant: "Sock Pro until {date}." With no subscription, the subscribe forms also show, so a human can subscribe before the grant ends.
- A billing customer: the "Manage billing" form, whatever the status, for invoices and payment methods.
- `?checkout=success` on the URL: "Payment received. Pro shows here once Stripe confirms it, usually within a minute; reload to check." `?checkout=cancel`: "Checkout cancelled. Nothing was charged." Any other value is ignored. Neither changes state: Pro comes only from the webhook.

Prices are text constants beside `stripe-setup.sh`'s amounts, checked equal by a unit test, not fetched from Stripe on each load.

### Rate limits

Both routes draw the session's inbox bucket (6 a minute): each makes one or two Stripe calls.

No RPC is added or changed; the proto is unchanged.

## Domain events

None. `BindBillingCustomer` emits none (Sock Pro billing); Pro still changes only through the webhook's events.

## Data layer

None. `identity.billing_customers` and the Pro source tables serve the new reads with their existing keys.

## Threats

| Threat | Mitigation |
|---|---|
| A cross-site form starting Checkout for a signed-in victim | `Origin` must be the hub's; `SameSite=Lax` sends no cookie on a cross-site `POST`. The worst case is still a Checkout page the victim has to complete |
| A stolen inbox cookie | It can start a Checkout or open the portal for that human for at most an hour. Checkout needs the holder to pay. The portal can cancel at period end, switch plans or change the card: the same reach as a stolen `sock_st_` session's `OpenBillingPortal`. Accepted: ADR 60 |
| A human billed twice | `StartCheckout`'s `ErrAlreadySubscribed` refusal, unchanged |
| An open redirect through the routes | The `303` targets come only from Stripe's API response or the constant `/inbox` |
| The return URL forged to claim payment | `?checkout=success` only prints a line; Pro comes from the signed webhook |
| Stripe errors leaking | `ErrBillingUnavailable` wraps them; the page is pass 1's fixed `503` |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| `Account.BindBillingCustomer` | `StartCheckout` RPC; `POST /inbox/subscribe` | none (unchanged) | `identity.billing_customers` insert (unchanged) |
| `Billing.CheckoutURL` | as above | none | none: Stripe's |
| `Billing.PortalURL` | `OpenBillingPortal` RPC; `POST /inbox/billing` | none | none: Stripe's |

## Tests

Black-box, with the fake WorkOS and the fake Stripe:

- A Basic human's page shows both subscribe forms and no Manage billing.
- `POST /inbox/subscribe` with `plan=yearly` is `303` to the fake's Checkout URL, and the fake saw one customer and a yearly session; a second post makes no second customer.
- A subscribed human's page shows the renewal date and Manage billing; their subscribe post is `303` to `/inbox`.
- `POST /inbox/billing` with a customer is `303` to the portal URL; without one, `303` to `/inbox`.
- A foreign `Origin` on either route is the fixed `400` and calls nothing in Stripe.
- No session: `303` to `/inbox`, nothing called.
- Terms not current: subscribe shows the terms page and calls nothing; billing still opens the portal.
- Billing off: both routes `404`; the page has no Pro section.
- `plan=weekly` and a doubled `plan` are the fixed `400`.
- A seventh request in a minute across `GET /inbox` and the routes is `429`.
- `?checkout=success` shows the line and changes nothing.
- The existing `StartCheckout` and `OpenBillingPortal` black-box tests pass unchanged.

Through the real path: on staging, sign in at `/inbox`, subscribe monthly with Stripe's test card in Checkout (Link shows), return to `/inbox`, reload until Pro shows, open Manage billing and cancel at period end.
