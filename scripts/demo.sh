#!/usr/bin/env bash
# Downloads a small BEIR dataset, indexes it with stored text, and serves the search page.
set -euo pipefail

usage() {
    echo "Usage: scripts/demo.sh [scifact|nfcorpus]" >&2
    echo "Set QUARRY_ADDR to change the listen address (default 127.0.0.1:8080)." >&2
    exit 1
}

if [[ $# -gt 1 ]]; then
    usage
fi
dataset=${1:-scifact}
case "$dataset" in
    scifact | nfcorpus) ;;
    *) usage ;;
esac
addr=${QUARRY_ADDR:-127.0.0.1:8080}

if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is not installed; see https://go.dev/doc/install" >&2
    exit 1
fi

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

echo "==> Downloading $dataset"
dataset_dir=$(scripts/download.sh "$dataset")

echo "==> Building quarry"
go build -o bin/ ./cmd/quarry-index ./cmd/quarry-serve

# Reuse an earlier demo index; quarry-index refuses to overwrite one.
index_dir="data/indexes/$dataset-demo"
if [[ -f "$index_dir/manifest.json" ]]; then
    echo "==> Using the existing index in $index_dir"
else
    rm -rf "$index_dir"
    echo "==> Indexing $dataset into $index_dir"
    bin/quarry-index --store-text --corpus "$dataset_dir/corpus.jsonl" --out "$index_dir"
fi

echo "==> Starting the server; open the address below in a browser. Press Ctrl-C to stop."
exec bin/quarry-serve --index "$index_dir" --addr "$addr"
