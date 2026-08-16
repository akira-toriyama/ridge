#!/usr/bin/env bash
# The local mirror of CI (the hub go-ci reusable). Green here == green there.
#
# Two CI-only lint findings reached CI in one day (PR #8, #10) because a bare
# `golangci-lint run` uses the smaller default linter set; the same class then
# recurred as VERSION skew (local 2.6.2 vs CI v2.12.2 — six releases of rule
# additions). So the linter list below is byte-identical to the CI invocation
# and the version is asserted, not assumed.
#
# Deliberate deltas from CI, named so "mirror" stays honest:
#   + gofmt guard        (CI has no format step; false-red only, cheap)
#   - go-bite            (needs a BASE..HEAD pair; inherently CI-only)
#   - linux+macos matrix (this box's OS only; ridge has no OS-conditional code)
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=local

# Keep in sync with the hub go-ci.yml `golangci-lint-version` default (@v2).
want="2.12.2"
have="$(golangci-lint version --short 2>/dev/null || true)"
if [ "$have" != "$want" ]; then
	echo "golangci-lint ${have:-missing} != CI's $want — fix with:" >&2
	echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$want" >&2
	exit 1
fi

go mod tidy -diff
go mod verify
go build ./...
go vet ./...
fmt_out="$(gofmt -l .)"
if [ -n "$fmt_out" ]; then
	printf '%s\n' "$fmt_out"
	echo "gofmt: files need formatting" >&2
	exit 1
fi
golangci-lint run --enable=errcheck,govet,ineffassign,misspell,revive,staticcheck,unconvert,unused,gosec
go test -race ./...
