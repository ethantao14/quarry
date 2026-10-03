#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: scripts/download.sh <scifact|nfcorpus|msmarco>" >&2
    exit 1
}

if [[ $# -ne 1 ]]; then
    usage
fi

dataset=$1
case "$dataset" in
    scifact) expected_md5=5f7d1de60b170fc8027bb7898e2efca1 ;;
    nfcorpus) expected_md5=a89dba18a62ef92f7d323ec890a0d38d ;;
    msmarco) ;;
    *) usage ;;
esac

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ "$dataset" == "msmarco" ]]; then
    data_dir="$repo_root/data/msmarco"
    archive="$data_dir/collectionandqueries.tar.gz"
    mkdir -p "$data_dir"

    if [[ ! -f "$archive" ]]; then
        curl -fL "https://msmarco.z22.web.core.windows.net/msmarcoranking/collectionandqueries.tar.gz" -o "$archive"
    fi

    # Microsoft publishes no checksum; this is the hash of the archive downloaded on 2026-10-03.
    expected_sha256=decac356eb8cc5b9cea2e30b8738dc6f367e4147aaefe8fc7526ddda382fd2fc
    if command -v sha256sum >/dev/null 2>&1; then
        actual_sha256=$(sha256sum "$archive")
    else
        actual_sha256=$(shasum -a 256 "$archive")
    fi
    actual_sha256=${actual_sha256%% *}

    if [[ "$actual_sha256" != "$expected_sha256" ]]; then
        rm -f "$archive"
        echo "Error: SHA-256 mismatch for collectionandqueries.tar.gz" >&2
        exit 1
    fi

    tar -xzf "$archive" -C "$data_dir" collection.tsv queries.dev.small.tsv qrels.dev.small.tsv
    printf '%s\n' "$data_dir"
    exit 0
fi

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
