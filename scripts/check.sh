#!/bin/sh
# Run the same checks CI would run. Usable two ways:
#   ./scripts/check.sh          — run on demand any time during development
#   .githooks/pre-push          — runs this automatically before every push (see that file)
#
# This exists because GitHub Actions minutes are constrained on a free account right now — this is
# the interim local enforcement standing in for real CI (see ../csvx-spec/AGENTS.md section 6).
set -e

cd "$(dirname "$0")/.."

# GUI git clients (and some non-interactive hook environments) launch hooks with a bare-bones PATH
# that doesn't include whatever your shell profile adds — so `go`/`gofmt` can be "not found" here
# even though they work fine from a terminal. Extend PATH with common install locations rather than
# trusting the inherited environment.
for dir in /usr/local/go/bin /opt/homebrew/bin /opt/homebrew/opt/go/bin "$HOME/go/bin" /usr/local/bin; do
	case ":$PATH:" in
	*":$dir:"*) ;;
	*) PATH="$PATH:$dir" ;;
	esac
done
export PATH

if ! command -v go >/dev/null 2>&1; then
	echo "check: 'go' not found even after extending PATH — if Go lives somewhere unusual on this" >&2
	echo "check: machine, add that directory to the list in $(basename "$0") (or in csvx-cli's copy)" >&2
	exit 1
fi

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
