# Contributing

1. Fork the repo and branch from `main`.
2. Make the change. Proto changes come with the regenerated `gen/` from `make build`.
3. Run `make check`. It must pass; CI runs the same target.
4. Open a pull request saying what changed and why. Keep each PR to one change.

Go code follows standard Go style: gofmt, `go vet` clean and idiomatic naming. See [AGENTS.md](AGENTS.md) for the repo's conventions.

Report security issues privately as [SECURITY.md](SECURITY.md) describes, not in a public issue or PR.

## License

sock-contract is MIT licensed. By contributing you agree your contribution is licensed under the same terms ([LICENSE](LICENSE)).
