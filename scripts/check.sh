#!/usr/bin/env bash
# The local mirror of CI: green here == green there. The linter list below is
# copied from the hub go-ci reusable's invocation — a bare `golangci-lint run`
# uses the smaller default set and let two revive findings reach CI (PR #8,
# #10) that never failed locally. Keep the list byte-identical to the CI log.
set -euo pipefail
cd "$(dirname "$0")/.."

go build ./...
go vet ./...
test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files need formatting"; exit 1; }
golangci-lint run --enable=errcheck,govet,ineffassign,misspell,revive,staticcheck,unconvert,unused,gosec
go test -race ./...
