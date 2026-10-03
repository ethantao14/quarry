#!/usr/bin/env bash
# Measures quarry-index wall time and peak memory for each worker count.
# Usage: scripts/bench-workers.sh [copies] [worker counts...]   (default: 20 1 2 4 6 8)
set -euo pipefail

copies=${1:-20}
shift || true
workers=("$@")
if [[ ${#workers[@]} -eq 0 ]]; then
    workers=(1 2 4 6 8)
fi
runs=3

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
source_corpus="$repo_root/data/beir/scifact/corpus.jsonl"
bench_dir="$repo_root/data/bench"
corpus="$bench_dir/scifact-x$copies.jsonl"

if [[ ! -f "$source_corpus" ]]; then
    echo "missing $source_corpus; run scripts/download.sh scifact first" >&2
    exit 1
fi
mkdir -p "$bench_dir"
# Each copy gets an ID prefix so every document ID stays unique.
if [[ ! -f "$corpus" ]]; then
    for ((copy = 1; copy <= copies; copy++)); do
        sed "s/^{\"_id\": \"/{\"_id\": \"c$copy-/" "$source_corpus"
    done > "$corpus.partial"
    mv "$corpus.partial" "$corpus"
fi

(cd "$repo_root" && go build -o bin/ ./cmd/quarry-index)

# GNU time reports peak memory in KB; BSD/macOS time reports bytes.
if /usr/bin/time -l true >/dev/null 2>&1; then
    time_flag=-l
else
    time_flag=-v
fi

median() {
    sort -n | awk '{ values[NR] = $1 } END { print values[int((NR + 1) / 2)] }'
}

echo "corpus: $corpus ($(wc -l < "$corpus" | tr -d ' ') docs), median of $runs runs"
printf "workers\tseconds\tpeak_rss_mb\n"
for w in "${workers[@]}"; do
    seconds=()
    rss=()
    for ((run = 1; run <= runs; run++)); do
        out="$bench_dir/idx-w$w"
        rm -rf "$out"
        log="$bench_dir/time-w$w.log"
        start=$(date +%s%N)
        /usr/bin/time "$time_flag" "$repo_root/bin/quarry-index" --corpus "$corpus" --out "$out" --workers "$w" > /dev/null 2> "$log"
        end=$(date +%s%N)
        seconds+=("$(awk -v ns=$((end - start)) 'BEGIN { printf "%.2f", ns / 1e9 }')")
        if [[ $time_flag == -l ]]; then
            rss+=("$(awk '/maximum resident set size/ { printf "%.0f", $1 / 1048576 }' "$log")")
        else
            rss+=("$(awk -F: '/Maximum resident set size/ { printf "%.0f", $2 / 1024 }' "$log")")
        fi
        rm -rf "$out"
    done
    printf "%s\t%s\t%s\n" "$w" "$(printf '%s\n' "${seconds[@]}" | median)" "$(printf '%s\n' "${rss[@]}" | median)"
done
