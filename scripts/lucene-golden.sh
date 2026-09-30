#!/usr/bin/env bash
# Regenerates the analyzer golden file using real Lucene as the reference.
# Needs Java 11+. Downloads Lucene into data/tools/lucene (gitignored) on first run.
set -euo pipefail

version=10.5.0
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
jar_dir="$repo_root/data/tools/lucene"
testdata="$repo_root/internal/analysis/testdata"
mkdir -p "$jar_dir"

for artifact in lucene-core lucene-analysis-common; do
    jar="$jar_dir/$artifact-$version.jar"
    url="https://repo1.maven.org/maven2/org/apache/lucene/$artifact/$version/$artifact-$version.jar"
    if [[ ! -f "$jar" ]]; then
        curl -fsSL "$url" -o "$jar"
    fi
    expected_sha1=$(curl -fsSL "$url.sha1" | cut -c1-40)
    actual_sha1=$(shasum -a 1 "$jar" | cut -c1-40)
    if [[ "$actual_sha1" != "$expected_sha1" ]]; then
        rm -f "$jar"
        echo "Error: SHA-1 mismatch for $artifact-$version.jar" >&2
        exit 1
    fi
done

java -cp "$jar_dir/lucene-core-$version.jar:$jar_dir/lucene-analysis-common-$version.jar" \
    "$repo_root/scripts/lucene-oracle/LuceneAnalyze.java" \
    < "$testdata/golden_inputs.txt" > "$testdata/golden_lucene.txt"
echo "Wrote $testdata/golden_lucene.txt"
