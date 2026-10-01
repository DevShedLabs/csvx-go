#!/bin/sh
# Run the same checks CI would run. Usable two ways:
#   ./scripts/check.sh          — run on demand any time during development
#   .githooks/pre-push          — runs this automatically before every push (see that file)
#
# This exists because GitHub Actions minutes are constrained on a free account right now — this is
# the interim local enforcement standing in for real CI (see ../csvx-spec/AGENTS.md section 6).
set -e

cd "$(dirname "$0")/.."

echo "check: gofmt..."
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "check: the following files need gofmt:" >&2
	echo "$unformatted" >&2
	echo "check: run 'gofmt -w .' and try again" >&2
	exit 1
fi

echo "check: go vet ./..."
go vet ./...

echo "check: go build ./..."
go build ./...

echo "check: go test ./..."
go test ./...

echo "check: all checks passed"
