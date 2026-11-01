# Contributing

sock-contract is the wire contract for Sock: protobuf services in `proto/sock/v1`, the Go, Swift and TypeScript generated from them in `gen/` and the specs in `docs/specs`. Every Sock client and the hub build against it, so a change here reaches all of them.

## Before you start

- Open an issue before anything beyond a small fix (a typo, a doc correction, an obvious bug). A wire change needs agreement on its shape before code.
- Report vulnerabilities privately as [SECURITY.md](SECURITY.md) describes. Never in an issue, pull request or discussion.

## Dev setup

| Tool | Version | Used for |
|---|---|---|
| Go | 1.26.2 or newer (`go.mod`; CI sets `go-version: "1.26"`) | build, tests, `protoc-gen-go`, `protoc-gen-connect-go` |
| buf | 1.68.4 (what CI installs) | lint, generate, breaking check |
| Node | 24 | TypeScript type check (`npm ci`, `tsc`) |
| Swift | 6.2 toolchain on macOS 26 | Swift plugins and tests (`Package.swift` targets macOS 26 and iOS 26) |

CI runs on `macos-26`. The Go protoc plugins are `tool` dependencies in `go.mod`, so `go tool` runs them with nothing to install. The Swift plugins (`protoc-gen-swift`, `protoc-gen-connect-swift`) are built from the Swift package dependencies by `make swift-plugins`. The TypeScript plugin is the remote `buf.build/bufbuild/es:v2.15.0`, so `make gen-ts` needs network access. There is no golangci-lint here: Go lint is gofmt and `go vet`.

| Target | Does |
|---|---|
| `make` / `make build` | Regenerates `gen/go`, `gen/swift` and `gen/ts`, then `go build ./...` |
| `make gen-go`, `make gen-swift`, `make gen-ts` | Regenerate one language |
| `make swift-plugins` | Builds the Swift protoc plugins into `.build/release` |
| `make lint` | `buf lint`, gofmt check, `go vet ./...` |
| `make test` | TypeScript type check, `go test ./...`, `swift test` |
| `make breaking` | `buf breaking` against the latest tag (`make breaking BASE=vX.Y.Z` for another) |
| `make check` | `lint`, `build`, `test`, `breaking`, then fails if `gen/` differs from what is committed |

`make install` and `make redeploy` print a note and do nothing: this is a library.

## Working across repos

This repo has no Sock dependencies. Its consumers ([sock-client](https://github.com/Aeryx-ai/sock-client), [sock-cli](https://github.com/Aeryx-ai/sock-cli), [sock-mcp](https://github.com/Aeryx-ai/sock-mcp)) pin it in `go.mod` to a public tag. To try a contract change in a consumer before it is tagged, use an untracked `go.work` in the consumer's checkout:

```sh
cd sock-cli
go work init .
go work use ../sock-contract
```

Delete `go.work` and `go.work.sum` when done. Never commit a `go.work` or a `replace` directive to any Sock repo. Once a new contract tag ships, the consumer bumps its pin in the change that first uses it:

```sh
go get github.com/aeryx-ai/sock-contract@vX.Y.Z
go mod tidy
```

## Making a change

1. Fork, then branch from `main`.
2. Edit `proto/` first, run `make build` and commit the regenerated `gen/` with it. Never edit `gen/` by hand.
3. Add or update tests. A bug fix starts with a test that fails without it. Wire tests live in `wire_test.go` (Go), `Tests/SockContractTests` (Swift) and `Tests/ts` (TypeScript). Run `go test -race ./...` when you touch Go code that runs concurrently. There are no fuzz tests in this repo.
4. Run `make check`. It must pass. CI runs the same target on every push and pull request.
5. Keep a PR to one change. A proto change, its regenerated code, its spec update and its tests belong together; an unrelated cleanup does not.

## Wire compatibility

The contract is deployed: old clients talk to new hubs and new clients to old hubs. Every change must stay wire compatible.

- Additive changes only: new messages, fields, RPCs and enum values.
- Never renumber a field. Never reuse a field number or name.
- When you remove a field or enum value, reserve its number and its name:

  ```proto
  reserved 7;
  reserved "old_name";
  ```

- `make breaking` (part of `make check`) runs `buf breaking --against '.git#tag=<latest tag>'` with the `FILE` rules and refuses anything else. It needs the tags: on a shallow clone run `git fetch --tags --unshallow` first.
- `buf lint` uses the `STANDARD` rules.
- `operator.proto` is excluded from Swift generation in `buf.gen.swift.yaml`. Keep it that way.

## Specs

`docs/specs` describes what the contract is, not how it was decided.

- `api.md` holds the rules proto cannot express: caller classes, errors, idempotency and rate limits. A new or changed RPC updates it in the same PR.
- `domain-model.md` and `events.md` hold the model and events behind the API.
- `cli.md` maps human RPCs to `sock` commands. `docs_test.go` fails if an RPC that a human or anonymous caller reaches in `api.md` has no command or stated exclusion in `cli.md`, or if `cli.md` names an RPC `api.md` does not define.

## Test vectors

`testdata/history-transfer` holds cross language vectors. `go-sealed.bin` is sealed by Go and opened by Swift; `swift-sealed.bin` is sealed by Swift to the same key and opened by Go. They must stay in sync. If you change the history transfer format, rewrite both in this order and commit them together:

```sh
go test ./historytransfer -run TestWriteGoVector -update
UPDATE_VECTORS=1 swift test --filter HistoryTransferTests
make check
```

The Swift vector is sealed to the Go vector's key, so Go goes first.

## Style

- Go: gofmt, `go vet` clean, `for i := range n` rather than a three clause loop for a plain count.
- Prose in specs and comments: terse and plain, no em or en dashes.

[AGENTS.md](AGENTS.md) has the same conventions in short form.

## Commit messages

- Imperative subject, short (about 50 characters, 72 at most), no trailing period: `Add read markers to Conversation`.
- A body that explains why, not what the diff already shows.
- One logical change per commit.
- Signed commits are encouraged: `git commit -S` or `git config commit.gpgsign true`. GPG and SSH keys both work.

## Pull requests

- Link the issue it resolves.
- Say what changed and why. Call out any wire change explicitly: which messages, fields or RPCs, and what consumers need to do.
- Include the regenerated `gen/`, the spec update and the tests.
- CI must be green before review. A maintainer reviews every PR; expect close questions on wire changes. Push review fixes as new commits; the maintainer may squash on merge.

## Releases (maintainers)

A release is a signed annotated `vX.Y.Z` tag on `main`, with the version as its message:

```sh
git tag -s v0.19.0 -m v0.19.0
git push origin v0.19.0
```

There is no release workflow. The tag is the release: Go consumers fetch it through the module proxy, Swift consumers through Swift Package Manager and TypeScript consumers by git tag. The next `make breaking` compares against it, so tag only a `main` that passes `make check`.

## License

MIT, copyright Aeryx Industries LLC ([LICENSE](LICENSE)). Inbound = outbound: by contributing you agree your contribution is licensed under the same terms.
