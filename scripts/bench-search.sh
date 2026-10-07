#!/usr/bin/env bash
# Compares exhaustive, WAND, and BMW search latency and verifies identical runs.
# Usage: scripts/bench-search.sh <index dir> <queries> <qrels>
set -euo pipefail

if [[ $# -ne 3 ]]; then
    echo "usage: scripts/bench-search.sh <index dir> <queries> <qrels>" >&2
    exit 1
fi
index=$1
queries=$2
qrels=$3
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bench_dir="$repo_root/data/bench"
mkdir -p "$bench_dir"

(cd "$repo_root" && go build -o bin/ ./cmd/quarry-eval)

printf "algo\tk\tMRR@10\tmean\tp50\tp95\tp99\n"
for k in 10 1000; do
    for algo in exhaustive wand bmw; do
        args=(--index "$index" --queries "$queries" --qrels "$qrels" --algo "$algo" --k "$k")
        run_path="$bench_dir/search-$algo-k$k.trec"
        "$repo_root/bin/quarry-eval" "${args[@]}" --run "$run_path" > /dev/null
        metrics=$("$repo_root/bin/quarry-eval" "${args[@]}" --run "$run_path")
        awk -F '\t' -v algo="$algo" -v k="$k" '
            { values[$1] = $2 }
            END {
                printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", algo, k, values["MRR@10"],
                    values["latency_ms_mean"], values["latency_ms_p50"],
                    values["latency_ms_p95"], values["latency_ms_p99"]
            }
        ' <<< "$metrics"
    done
done
for k in 10 1000; do
    cmp "$bench_dir/search-exhaustive-k$k.trec" "$bench_dir/search-wand-k$k.trec"
    cmp "$bench_dir/search-exhaustive-k$k.trec" "$bench_dir/search-bmw-k$k.trec"
    echo "k=$k: runs identical"
done
