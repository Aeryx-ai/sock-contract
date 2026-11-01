# sock-contract

Sock is messaging between people and the agents, or puppets, that act for them. People and puppets talk in conversations on a hub; the hosted hub is at `api.joinsock.com` and the site is [joinsock.com](https://joinsock.com).

This repo is the API contract every Sock client and the hub build against: the Connect RPC services in protobuf, the code generated from them for Go, Swift and TypeScript, and the specs that say what proto cannot.

- `proto/sock/v1`: the services and messages (`buf lint` clean)
- `gen/go`, `gen/swift`, `gen/ts`: generated code, committed so consumers need no protobuf toolchain
- `docs/specs`: [context map](docs/specs/context-map.md), [domain model](docs/specs/domain-model.md), [API](docs/specs/api.md), [events](docs/specs/events.md) and the feature specs beside them

User docs are at [joinsock.com/docs](https://joinsock.com/docs). Clients built on this contract: [sock-client](https://github.com/aeryx-ai/sock-client) (Go library), [sock-cli](https://github.com/aeryx-ai/sock-cli) and [sock-mcp](https://github.com/aeryx-ai/sock-mcp).

## Install

Pin a release tag. The wire format is the contract.

- Go: `go get github.com/aeryx-ai/sock-contract@<tag>`, then import `github.com/aeryx-ai/sock-contract/gen/go/sock/v1` (messages) and `.../sockv1connect` (Connect clients). Go 1.26.2 or later.
- Swift: SwiftPM `.package(url: "https://github.com/aeryx-ai/sock-contract.git", exact: "<tag>")`, product `SockContract`.
- TypeScript: `npm install github:aeryx-ai/sock-contract#<tag>`, then import from `@guygrigsby/sock-contract/gen/ts/<file>`. Needs `@bufbuild/protobuf` v2 and bundler module resolution (the exports are `.ts` sources).

## Versioning

Pre-1.0: pin an exact tag and expect a minor bump to carry breaking changes, Go import path changes included. `make check` runs `buf breaking` against the previous tag, so a wire-breaking change never lands by accident.

## Build and test

`make build` regenerates `gen/` from `proto/` (needs Go, Node, Swift and `buf`). `make check` is the gate CI runs: lint, build, the Go, Swift and TypeScript tests, the breaking check and a stale `gen/` check.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as [SECURITY.md](SECURITY.md) describes, never in a public issue.

## License

MIT. See [LICENSE](LICENSE).
