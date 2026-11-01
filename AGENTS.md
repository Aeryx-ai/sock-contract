# Agent and contributor guide

sock-contract is the wire contract for Sock: protobuf services in `proto/sock/v1`, the code generated from them in `gen/` and the specs in `docs/specs`.

## Build and test

- `make build` regenerates `gen/go`, `gen/swift` and `gen/ts` from `proto/` and builds the Go module. Needs Go, Node, Swift and `buf`.
- `make check` is the gate CI runs: `buf lint`, gofmt, `go vet`, the Go, Swift and TypeScript tests, `buf breaking` against the latest tag and a stale `gen/` check. It must pass before every commit.

## Conventions

- Change `proto/` first, run `make build` and commit the regenerated `gen/` with it. Never edit `gen/` by hand.
- Wire changes are additive: `buf breaking` against the latest tag refuses anything else.
- Specs in `docs/specs` describe what the contract is, not how it was decided. `api.md` holds the rules proto cannot express (caller classes, errors, idempotency, rate limits); `domain-model.md` and `events.md` hold the model and events behind them. `docs_test.go` keeps `cli.md` in step with `api.md`.
- Go: Go 1.26, gofmt, `for range n` for plain counts.
- Prose: terse and plain, no em or en dashes.
