.DEFAULT_GOAL := build
.PHONY: build swift-plugins gen-go gen-swift gen-ts test test-ts lint breaking check install redeploy

build: gen-go gen-swift gen-ts
	go build ./...

gen-go:
	buf generate --template buf.gen.go.yaml

swift-plugins:
	swift build -c release --product protoc-gen-swift
	swift build -c release --product protoc-gen-connect-swift

gen-swift: swift-plugins
	buf generate --template buf.gen.swift.yaml

gen-ts:
	buf generate --template buf.gen.ts.yaml

lint:
	buf lint
	gofmt -l . | (! grep .)
	go vet ./...

test: test-ts
	go test ./...
	swift test

node_modules: package.json package-lock.json
	npm ci
	@touch node_modules

test-ts: node_modules
	npx tsc -p tsconfig.json

BASE ?= $(shell git describe --tags --abbrev=0 2>/dev/null)

breaking:
	@if [ -n "$(BASE)" ]; then buf breaking --against '.git#tag=$(BASE)'; else echo "no tag yet, skipping breaking check"; fi

check: lint build test breaking
	git diff --exit-code -- gen/ || (echo "generated code is stale: run make build and commit" && exit 1)
	git status --porcelain gen/ | (! grep .) || (echo "generated code is stale: run make build and commit" && exit 1)

install:
	@echo "contract is a library; consumers pin a tag"

redeploy:
	@echo "contract has nothing to deploy"
