#!/bin/sh
# Example pre-commit hook. Install with:
#   ln -s ../../scripts/pre-commit.sh .git/hooks/pre-commit
set -e

echo "==> gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    echo "Unformatted files:" >&2
    echo "$unformatted" >&2
    echo "Run 'make fmt' to fix." >&2
    exit 1
fi

echo "==> go vet"
go vet ./...

if command -v golangci-lint >/dev/null 2>&1; then
    echo "==> golangci-lint"
    golangci-lint run
else
    echo "(golangci-lint not installed, skipping)"
fi
