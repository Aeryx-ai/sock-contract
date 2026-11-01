# Google and passkey sign-in on desktop

Approved, pass 1. Contexts: Identity. Builds on [Google and passkey sign-in](google-and-passkey-sign-in.md), whose RPCs, tables and rules stay in force unchanged, and on the loopback handoff desktop Sign in with Apple and `sock login` already use (the `cli` routes in [api](api.md), Operational), with its mandatory warning page.

What changes:

- The Linux and Windows app offers "Sign in with passkey" and "Continue with Google" beside email and Apple, and Settings offers "Add a passkey" and "Add Google sign-in", as the Mac app does.
- Both run in the system browser and hand their result to the app's `127.0.0.1` listener, as desktop Sign in with Apple already does. The app never embeds a sign-in page.
- No proto, event or table changes. The hub gains two anonymous routes for Google; joinsock.com gains one static page for passkeys.

Why the browser: WorkOS production refuses `http` and `localhost` redirect URIs, so Google cannot return to the loopback directly. A webview's origin is not on `joinsock.com`, so WebAuthn inside the app fails the relying party check. Linux has no platform passkey store an app could call.

## Ubiquitous language

| Term | Meaning | Stored or derived |
|---|---|---|
| Loopback listener | The desktop app's one-shot HTTP listener on `127.0.0.1:<port>`, as for Apple sign-in. It accepts only a request carrying its own `state` | Not stored |
| Flow state | `<port>.<43 base64url characters of 32 random bytes>`: the listener's port, then a secret only the app holds, as for Apple sign-in | Not stored |
| Google relay | The hub's warning page and callback that carry a WorkOS code for Google from WorkOS to the loopback listener | Not stored: the hub keeps no state |
| Passkey page | A static page at `https://joinsock.com/passkey` that runs one WebAuthn ceremony in the browser for relying party `joinsock.com` and hands the result to the loopback listener | Not stored |

## Domain model

No change. Desktop Google sign-in is `SignInWithGoogle` with a code and verifier; desktop passkeys are `StartPasskeySignIn` and `SignInWithPasskey`, `StartPasskeyRegistration` and `FinishPasskeyRegistration`, exactly as the apps call them. The only new rule the hub checks is the origin: `https://joinsock.com` is already in `SOCK_PASSKEY_ORIGINS`.

## Configuration

| Variable or setting | Meaning |
|---|---|
| `SOCK_WORKOS_DESKTOP_REDIRECT_URI` | New. Exactly `https://<hub host>/workos/desktop-callback`, no port, query or fragment. Production `https://api.joinsock.com/workos/desktop-callback`; staging, on the host a browser reaches the staging hub on. Empty, or WorkOS unconfigured: both Google relay routes answer `404`. sockd refuses any other shape |
| WorkOS dashboard, per environment | The desktop redirect URI registered beside the app callback |
| `SOCK_PASSKEY_ORIGINS` | Unchanged: `https://joinsock.com` covers the passkey page in every environment |
| joinsock.com | Serves `/passkey` and its script from `'self'` |

## Caller classes

Unchanged. The two hub routes are anonymous, like `/apple/cli-login` and `/apple/cli-callback`: they touch no database and call nothing, so they reach nothing an anonymous caller could not already reach. The desktop app is a human device once signed in, as today.

## API

### Hub routes (Operational, added to [api](api.md))

Anonymous, on every listener, no database or WorkOS call, not rate limited by sockd. Every response carries `Cache-Control: no-store`, `Referrer-Policy: no-referrer` and `Content-Security-Policy: default-src 'none'`. Nothing logs their query string. Anything that breaks a rule below is the fixed `400` page.

| Route | Request | Response |
|---|---|---|
| `GET /workos/desktop-login` | `state` matching `^[0-9]{4,5}\.[A-Za-z0-9_-]{43}$` whose leading integer is 1024 to 65535; `code_challenge` matching `^[A-Za-z0-9_-]{43}$` (S256 of the app's verifier) | `200` static page, no script: "Continue only if you just chose Continue with Google in the Sock app on this computer. Someone may be trying to sign in to your account." One link, to WorkOS's authorize URL with `response_type=code`, `client_id` = `SOCK_WORKOS_CLIENT_ID`, `redirect_uri` = `SOCK_WORKOS_DESKTOP_REDIRECT_URI`, `provider=GoogleOAuth`, the given `state` and `code_challenge`, and `code_challenge_method=S256`. Sets cookie `__Host-sock-desktop-google` = lowercase hex SHA-256 of `state`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Max-Age=600` |
| `GET /workos/desktop-callback` (the path `SOCK_WORKOS_DESKTOP_REDIRECT_URI` must have) | WorkOS's redirect: `state` (same rule), and either `code` of 1 to 512 printable ASCII, or `error` matching `^[a-z_]{1,64}$` (`error_description` ignored) | Only when `__Host-sock-desktop-google` equals the SHA-256 of `state`: `303` to `http://127.0.0.1:<port>/google?state=…&code=…` (or `&error=…`), port from `state`. Always expires the cookie. Without the cookie, the fixed `400` |

`SameSite=Lax` suffices: WorkOS returns the browser with a top-level `GET` redirect, on which browsers send `Lax` cookies. Apple's warning page needs `None` only because Apple posts a form.

### Passkey page (joinsock.com)

`GET https://joinsock.com/passkey`: a static page with one script from `'self'`. It reads its input from the URL fragment only, so neither the CDN nor any log sees it:

| Fragment field | Ceremony | Meaning |
|---|---|---|
| `op` | both | `get` (sign in) or `create` (add a passkey) |
| `port`, `state` | both | The listener's port (1024 to 65535) and flow state, as above; `port` must equal the state's leading integer |
| `challenge` | both | The hub's 32-byte challenge, base64url |
| `user_handle`, `user_name` | `create` | From `StartPasskeyRegistration`: 16 bytes base64url; `@handle` |
| `exclude` | `create` | Comma-separated base64url credential ids from `StartPasskeyRegistration`, 0 to 10 |

Any field missing or malformed: the page says "This link is broken. Start again from the Sock app." and runs nothing.

Otherwise it shows "Continue only if you just chose to use a passkey in the Sock app on this computer." and one Continue button. The ceremony runs only from that click (WebAuthn needs a user gesture in Safari anyway, so the warning cannot be skipped):

- `get`: `navigator.credentials.get` with `publicKey` `{challenge, rpId: "joinsock.com", userVerification: "required"}` and no allow list.
- `create`: `navigator.credentials.create` with `publicKey` `{challenge, rp: {id: "joinsock.com", name: "Sock"}, user: {id: user_handle, name: user_name, displayName: user_name}, pubKeyCredParams: [ES256, RS256], authenticatorSelection: {residentKey: "required", userVerification: "required"}, attestation: "none", excludeCredentials: exclude}`.

The RP ID is fixed in the page, never read from the fragment. On success the page replaces itself (`location.replace`) with `http://127.0.0.1:<port>/passkey?state=…` plus, base64url without padding, `credential_id`, `client_data_json` and either `authenticator_data`, `signature`, `user_handle` (`get`) or `attestation_object` (`create`). On `NotAllowedError` it sends `error=cancelled`; on anything else `error=failed`; without `PublicKeyCredential`, `error=unsupported`. The page sends nothing anywhere else.

### Loopback listener (sock-client `login`)

`sock-client`'s `login` package, which runs desktop Apple sign-in today, gains `Google` and `Passkey` runs that share its listener rules: bind `127.0.0.1` before building the URL, accept the first request whose `Host` is `127.0.0.1:<port>` and whose single `state` matches in constant time, `400` and keep waiting on anything else, `303` to `/done`, 10 minute timeout.

| Route | Accepts | Hands back |
|---|---|---|
| `GET /google` | `code` (1 to 512 printable ASCII) or `error` | The code with the run's verifier, for `SignInWithGoogle` or `AddEmailSignIn` with `google`. An `error` is reported as cancelled or failed, never echoed |
| `GET /passkey` | The fields above, each valid base64url within the contract's sizes (credential id 16 to 1023 bytes, `client_data_json` at most 4 KiB, `attestation_object` at most 16 KiB), or `error` | The bytes for `SignInWithPasskey` or `FinishPasskeyRegistration`. `cancelled` is no error |

The CLI can use the same runs later; this spec does not add CLI commands.

## Domain events

None.

## Data layer

None.

## Desktop app

1. Sign-in screen, in order: "Sign in with passkey", "Continue with Google" (shown when `GET /workos/desktop-login` with no query answers `400`, meaning configured, not `404`), "Continue with email" (primary), "Sign in with Apple".
2. Passkey: `StartPasskeySignIn`, refuse an `rp_id` other than `joinsock.com`, open the passkey page with `op=get`, wait on the listener, then `SignInWithPasskey`. While waiting: "Finish in your browser." with Cancel. Cancelled: back with no error. `PASSKEY_INVALID`: the Mac app's sentence. `unsupported`: "This browser can't use passkeys. Sign in another way."
3. Google: make a PKCE verifier, open `GET /workos/desktop-login`, wait, then `SignInWithGoogle`. A `registration` answer opens email pass 3's "Create your account" screen with the ticket.
4. Settings, "Passkeys": `ListPasskeys` with remove, and "Add a passkey": `StartPasskeyRegistration`, the passkey page with `op=create`, `FinishPasskeyRegistration` with the device label.
5. Settings, "Add Google sign-in" for a human without an email identity: the Google run, then `AddEmailSignIn` with `google`.

## Threats

| Threat | Mitigation |
|---|---|
| Open redirect on the hub | Neither route takes a URL: the page links only to WorkOS's fixed authorize URL, the callback redirects only to `http://127.0.0.1:<port>/google`, port parsed from a `state` of the fixed shape |
| A crafted link straight to WorkOS that skips the warning | The callback relays only with the cookie the warning page set for that `state`, as Apple's does |
| A Google code read on the loopback hop | Useless alone: redeeming it needs the PKCE verifier, which never leaves the app process. The listener's `303` to `/done` clears the address bar |
| A passkey assertion read on the loopback hop | Bound to a single-use challenge the app asked for and spends within seconds; a second presentation is `PASSKEY_INVALID` |
| A hostile local listener on `127.0.0.1` (malware, or another OS user's process) that starts its own Google or passkey flow and opens the page (the RFC 8252 residual of the loopback handoff) | It holds its own verifier or challenge, so it gets a session if the person clicks through the warning and finishes Google or approves their passkey. The page's warning and the browser's own passkey prompt naming `joinsock.com` are the mitigation. Accepted, as it is for Apple |
| Script on another joinsock.com page running ceremonies | joinsock.com is a static site with no user content. Any script on it could already run WebAuthn for `joinsock.com`; the page adds no new power |
| The fragment leaking | A fragment is never sent to a server or in `Referer`. It holds a public challenge, the port, the state and, for `create`, the handle |

## Cross-check

| Transition | API | Event | DDL |
|---|---|---|---|
| Google sign-in from desktop | `SignInWithGoogle`, through the two hub routes | as the apps | as the apps |
| `Account.AddEmailIdentity` (Google, desktop) | `AddEmailSignIn` with `google` | none | as the apps |
| Passkey sign-in from desktop | `StartPasskeySignIn`, the passkey page, `SignInWithPasskey` | as the apps | as the apps |
| `Account.AddPasskey` from desktop | `StartPasskeyRegistration`, the passkey page, `FinishPasskeyRegistration` | none | as the apps |
| `Account.RemovePasskey` from desktop | `RemovePasskey` | none | as the apps |

## Open questions

- Chrome's Local Network Access permission: desktop Apple sign-in already relies on top-level navigation from a public page to `127.0.0.1`. The passkey page makes the same navigation from script (`location.replace`) instead of a `303`. Confirm in Chrome, Edge, Firefox and Safari on staging before release.
- Whether Firefox on Linux offers phone passkeys (hybrid, QR) or only security keys. Chrome and Edge offer both.
- The staging redirect URI: confirm WorkOS's staging environment accepts its host.
