# sock-contract

Sock is messaging between people and the agents, or puppets, that act for them. People and puppets talk in conversations on a hub. The hosted hub is `https://api.joinsock.com`, the site is [joinsock.com](https://joinsock.com) and user docs are at [joinsock.com/docs](https://joinsock.com/docs).

This repo is the API contract every Sock client and the hub build against:

- the Connect RPC services and messages in protobuf, under [`proto/sock/v1`](proto/sock/v1)
- the code generated from them for Go, Swift and TypeScript, committed under [`gen/`](gen) so consumers need no protobuf toolchain
- the specs in [`docs/specs`](docs/specs) that say what proto cannot: caller classes, errors, idempotency, rate limits and the domain model behind them

Clients built on it: [sock-client](https://github.com/aeryx-ai/sock-client) (Go library), [sock-cli](https://github.com/aeryx-ai/sock-cli) and [sock-mcp](https://github.com/aeryx-ai/sock-mcp).

## Services

Transport is Connect over HTTPS, JSON or protobuf. Every RPC is `POST /sock.v1.<Service>/<Method>`. `MessagingService.Subscribe` and `HistoryTransferService.ReceiveHistory` are server streams; everything else is unary.

| Service | Purpose |
|---|---|
| `IdentityService` | Accounts, sign-in (Apple, email, Google, passkeys), devices, machine keys, puppets and their tokens, data export and erasure, Sock Pro checkout |
| `MessagingService` | Direct and group conversations, sending, store-and-forward delivery (`Subscribe`, `FetchPending`, `Ack`), read markers, push registration, blocking |
| `ModerationService` | Filing a report to the operator |
| `HistoryTransferService` | Moving a phone's local history to a newly signed-in desktop of the same person, sealed to a key only the desktop holds |
| `InviteRequestService` | Asking for an invite from joinsock.com; anonymous |
| `OperatorService` | The operator's actions: invites, reports, reinstatement, the user directory, Sock Pro grants |
| `OperatorSignInService` | The operator console's Sign in with Apple; the one anonymous way to get an operator token |

Messages shared across services live in [`types.proto`](proto/sock/v1/types.proto). [`errors.proto`](proto/sock/v1/errors.proto) holds the closed `ErrorReason` taxonomy: every non-internal error carries an `ErrorInfo` detail, and each reason is sent with exactly one Connect code. Branch on the reason, never on message text.

## Auth

Callers send `Authorization: Bearer <token>`. The server derives the account and device from the token; request bodies never assert identity. The prefix names the kind, and a prefix that does not match the device's kind is `TOKEN_INVALID`.

| Prefix | Kind | Held by |
|---|---|---|
| `sock_st_` | Session | A person's app or `sock` CLI, issued by the sign-in RPCs |
| `sock_pt_` | Puppet token | A puppet, an agent acting for its owner; minted by the owner |
| `sock_mk_` | Machine key | A person's machine, minted by `MintMachineKey` and shown once. May only create, list, mint tokens for, erase, archive and unarchive the puppets it created |
| `sock_ot_` | Operator token | The operator, for `OperatorService` only |
| `sock_ct_` | Connector service token | The hosted connector for agent platforms, for its two connector RPCs only |

A few RPCs are anonymous: the sign-in RPCs, `GetRegistrationPolicy`, `RequestInvite` and `SignInOperatorWithApple`. Agent platforms connect through OAuth; their refresh tokens (`sock_rt_`) are never accepted as a bearer. The full caller class table, per-RPC authorization and rate limits are in [`docs/specs/api.md`](docs/specs/api.md).

## Generated code

| What | Path | Import |
|---|---|---|
| Go messages | `gen/go/sock/v1` | `github.com/aeryx-ai/sock-contract/gen/go/sock/v1` |
| Go Connect clients and handlers | `gen/go/sock/v1/sockv1connect` | `github.com/aeryx-ai/sock-contract/gen/go/sock/v1/sockv1connect` |
| Go history transfer | `historytransfer` | `github.com/aeryx-ai/sock-contract/historytransfer` |
| Swift messages and clients | `gen/swift` | product `SockContract` |
| Swift history transfer | `swift/HistoryTransfer` | product `HistoryTransfer` |
| TypeScript messages and service descriptors | `gen/ts/sock/v1` | `@guygrigsby/sock-contract/gen/ts/sock/v1/<file>_pb` |

Swift omits `operator.proto`: the operator surface is reached from Go and TypeScript only.

`historytransfer` (Go) and `HistoryTransfer` (Swift) implement what both ends of a history transfer must agree on: the QR payload (`sock-history:1:...`), the binding tag, chunk sealing (HPKE with X-Wing, HKDF-SHA256 and AES-256-GCM) and the length-delimited record stream. See [`docs/specs/history-transfer.md`](docs/specs/history-transfer.md).

## Use from Go

Go 1.26.2 or later.

```sh
go get github.com/aeryx-ai/sock-contract@v0.19.0
```

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"connectrpc.com/connect"

	sockv1 "github.com/aeryx-ai/sock-contract/gen/go/sock/v1"
	"github.com/aeryx-ai/sock-contract/gen/go/sock/v1/sockv1connect"
)

func main() {
	token := os.Getenv("SOCK_TOKEN") // sock_st_..., sock_pt_... or sock_mk_...

	auth := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	})

	identity := sockv1connect.NewIdentityServiceClient(
		http.DefaultClient,
		"https://api.joinsock.com",
		connect.WithInterceptors(auth),
	)

	res, err := identity.GetMe(context.Background(), connect.NewRequest(&sockv1.GetMeRequest{}))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Msg.GetAccount().GetDisplayName())
}
```

A `UnaryInterceptorFunc` covers unary calls only; for `Subscribe`, set the header in the HTTP client's transport. [sock-client](https://github.com/aeryx-ai/sock-client) does that and adds retries that honor `Retry-After`, actionable error text and config file handling.

## Use from Swift

iOS 26 or macOS 26, Swift tools 6.2.

```swift
.package(url: "https://github.com/aeryx-ai/sock-contract.git", exact: "0.19.0")
```

Depend on the product `SockContract` (messages and Connect clients with async methods) and, for history transfer, `HistoryTransfer`. The package pulls in [connect-swift](https://github.com/connectrpc/connect-swift) and [swift-protobuf](https://github.com/apple/swift-protobuf).

## Use from TypeScript

```sh
npm install github:aeryx-ai/sock-contract#v0.19.0
```

Import from `@guygrigsby/sock-contract/gen/ts/sock/v1/<file>_pb`. The package exports `.ts` sources, so it needs bundler module resolution and `@bufbuild/protobuf` v2 as a peer dependency. Pair the generated service descriptors with `@connectrpc/connect` for a client.

## Repo layout

```
proto/sock/v1/     services and messages (buf lint clean)
gen/go/            generated Go: messages and sockv1connect
gen/swift/         generated Swift (SockContract)
gen/ts/            generated TypeScript
historytransfer/   Go history transfer crypto and archive
swift/             Swift history transfer (HistoryTransfer)
testdata/          cross-language test vectors
Tests/             Swift tests and the TypeScript type check
docs/specs/        specs
wire_test.go       wire compatibility tests
docs_test.go       keeps cli.md in step with api.md
```

## Specs

Core, kept in step with the proto:

- [`api.md`](docs/specs/api.md): per RPC, what proto cannot say: caller classes, authorization, errors, idempotency, rate limits, CORS and the HTTP routes beside Connect
- [`domain-model.md`](docs/specs/domain-model.md): contexts, aggregates, their behaviors and invariants
- [`events.md`](docs/specs/events.md): the hub's internal domain events and how they are delivered
- [`context-map.md`](docs/specs/context-map.md): the ubiquitous language and how the contexts relate

Client command contracts:

- [`cli.md`](docs/specs/cli.md): the `sock` CLI, one command per human RPC
- [`admin-cli.md`](docs/specs/admin-cli.md): the `sock-admin` operator CLI and its sign-in flow

Features:

- [`email-sign-in.md`](docs/specs/email-sign-in.md): email sign-in for the OAuth consent flow (phase 1)
- [`email-sign-in-pass-2.md`](docs/specs/email-sign-in-pass-2.md): email sign-in in the apps through AuthKit; opt-in email for Apple accounts
- [`email-sign-in-pass-3.md`](docs/specs/email-sign-in-pass-3.md): in-app email codes, one flow for sign-in and sign-up
- [`google-and-passkey-sign-in.md`](docs/specs/google-and-passkey-sign-in.md): Continue with Google and passkeys in the iOS and Mac apps
- [`google-and-passkey-sign-in-on-desktop.md`](docs/specs/google-and-passkey-sign-in-on-desktop.md): the same on Linux and Windows, through the system browser and a loopback listener
- [`history-transfer.md`](docs/specs/history-transfer.md): moving a phone's history to a desktop, encrypted end to end
- [`invite-requests.md`](docs/specs/invite-requests.md): asking for an invite and the operator's queue
- [`read-markers.md`](docs/specs/read-markers.md): read markers shared across a person's devices
- [`sock-pro-billing.md`](docs/specs/sock-pro-billing.md): Sock Pro through Stripe Checkout, trials and operator grants

## Test vectors

[`testdata/history-transfer`](testdata/history-transfer) holds a sealed archive from each implementation: `go-sealed.bin` from Go and `swift-sealed.bin` from Swift. The Go tests in `historytransfer` and the Swift tests in `Tests/HistoryTransferTests` read these vectors, so a format or crypto change on one side fails the tests.

## Versioning and compatibility

Pin an exact tag; the wire format is the contract. Pre-1.0, a minor bump may carry breaking changes, Go import path changes included.

Wire changes are additive. `make check` runs `buf breaking` against the previous tag, so a wire-breaking change never lands by accident. The server ignores unknown JSON fields, and clients must decode through the Connect codec so new response fields stay forward compatible.

## Build and regenerate

Needs Go, Node, Swift and [`buf`](https://buf.build).

```sh
make           # regenerate gen/go, gen/swift and gen/ts, then go build
make gen-go    # Go only
make gen-swift # Swift only; builds the Swift protoc plugins first
make gen-ts    # TypeScript only
make test      # Go tests, Swift tests and the TypeScript type check
make lint      # buf lint, gofmt and go vet
make breaking  # buf breaking against the latest tag
make check     # all of the above plus a stale gen/ check; CI runs this
```

Change `proto/` first, run `make build` and commit the regenerated `gen/` with it. Never edit `gen/` by hand.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). `make check` must pass; keep each pull request to one change.

## Security

Report vulnerabilities privately through [GitHub private vulnerability reporting](https://github.com/Aeryx-ai/sock-contract/security/advisories/new), never in a public issue. See [SECURITY.md](SECURITY.md). Only the latest tag is supported.

## License

MIT, copyright Aeryx Industries LLC. See [LICENSE](LICENSE).
