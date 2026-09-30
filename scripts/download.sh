#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: scripts/download.sh <scifact|nfcorpus>" >&2
    exit 1
}

if [[ $# -ne 1 ]]; then
    usage
fi

dataset=$1
case "$dataset" in
    scifact) expected_md5=5f7d1de60b170fc8027bb7898e2efca1 ;;
    nfcorpus) expected_md5=a89dba18a62ef92f7d323ec890a0d38d ;;
    *) usage ;;
esac

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
data_dir="$repo_root/data/beir"
archive="$data_dir/$dataset.zip"
mkdir -p "$data_dir"

if [[ ! -f "$archive" ]]; then
    curl -fL "https://public.ukp.informatik.tu-darmstadt.de/thakur/BEIR/datasets/$dataset.zip" -o "$archive"
fi

if command -v md5sum >/dev/null 2>&1; then
    actual_md5=$(md5sum "$archive")
    actual_md5=${actual_md5%% *}
else
    actual_md5=$(md5 -q "$archive")
fi

if [[ "$actual_md5" != "$expected_md5" ]]; then
    rm -f "$archive"
    echo "Error: MD5 mismatch for $dataset.zip" >&2
    exit 1
fi

unzip -q -o "$archive" -d "$data_dir"
printf '%s\n' "$data_dir/$dataset"
