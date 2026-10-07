# quarry

A full-text search engine written from scratch in Go. It builds an inverted index on disk, ranks
results with BM25, and speeds up top-k retrieval with dynamic pruning (WAND and Block-Max WAND).
It is evaluated on the public MS MARCO and BEIR benchmarks against published BM25 baselines.

**Status:** English text analysis, BM25 search, evaluation, and on-disk indexes (built within a
memory budget, read back with memory mapping) work, including the full MS MARCO passage corpus.
WAND and Block-Max WAND retrieval are implemented and return identical results to exhaustive search.

## Requirements

- Go 1.27.1 or newer
- [golangci-lint](https://golangci-lint.run/) v2 (only needed for `make lint`)

## Usage

```sh
git clone https://github.com/ethantao14/quarry.git
cd quarry
go build -o bin/quarry-search ./cmd/quarry-search
./bin/quarry-search --corpus cmd/quarry-search/testdata/tiny.jsonl --k 2 fish
```

Use a BEIR JSONL corpus with `_id`, `title`, and `text` fields, or a TSV file ending in `.tsv`
with one `id<TAB>text` document per line (the MS MARCO format). Results are tab-separated:
`rank` (1-based), `external document ID`, and `score` (four decimal places).

To analyze the corpus once instead of on every search, save an index and search that:

```sh
go build -o bin/ ./cmd/...
./bin/quarry-index --corpus cmd/quarry-search/testdata/tiny.jsonl --out data/indexes/tiny
./bin/quarry-search --index data/indexes/tiny --k 2 fish
```

`quarry-index` prints the document count, distinct term count, total postings, number of
temporary segments, total index size in bytes, build time in seconds, documents per second, and
bits per posting (postings file size divided by postings). It refuses to overwrite an existing directory, and removes its partial
output if indexing fails.

To index a corpus larger than memory, set `--mem-budget` (bytes, or a number with `KB`, `MB`, or
`GB`; default `1GB`). Postings are collected in memory until they reach a third of the budget; then
quarry writes them as a temporary segment and continues, and at the end it merges the segments into
one index. The result is byte-for-byte the same for any budget. From 256 MB up, the budget is also
Go's soft memory limit, so the whole build stays within it: on MS MARCO, peak memory was 79% of a
1 GB budget and 88% of a 512 MB budget. At 256 MB it went 16% over, because the final merge needs
about 100 MB for MS MARCO's term dictionary whatever the budget (see
[docs/DESIGN.md](docs/DESIGN.md#indexing-within-a-memory-budget)). Smaller budgets only size the
chunks.

Text analysis runs on `--workers` goroutines (default: the number of CPUs Go uses). The index is
byte-for-byte the same for any worker count. Use exactly one of `--corpus` or `--index` when
searching. Results from a saved index are identical to searching the corpus directly. Saved indexes
are memory-mapped, which works on macOS and Linux. The format (version 2: postings in blocks of 128
with skip entries and BM25 score bounds) is described in
[docs/DESIGN.md](docs/DESIGN.md#on-disk-index-format). An index saved by an older build reports its
format version and must be rebuilt with `quarry-index`.

`quarry-bench --index <dir> --queries <path>` benchmarks search latency and throughput,
with hardware details and configurable algorithms, result counts, and concurrent clients.

## Evaluation

Both `quarry-search` and `quarry-eval` accept `--algo bmw` (the default), `--algo wand`, or
`--algo exhaustive`. All three return identical document IDs, ordering, and scores.
WAND uses term score bounds to skip candidates; Block-Max WAND also skips using tighter block bounds.
Both require the same BM25 parameters used to compute the index's score bounds.

```sh
scripts/download.sh scifact
go run ./cmd/quarry-eval --dataset data/beir/scifact --run runs/scifact.trec --k 1000
```

The same works for NFCorpus: replace `scifact` with `nfcorpus` in both commands. To evaluate a
saved index, add `--index <dir>`; queries and judgments still come from `--dataset`, unless
`--queries` (BEIR JSONL, or `id<TAB>text` when the file ends in `.tsv`) and `--qrels` (BEIR TSV with
a header, or TREC `qid iteration docid grade`) point elsewhere.

MS MARCO passage ranking (8.8M passages, about 1 GB to download and 3 GB unpacked):

```sh
scripts/download.sh msmarco
go build -o bin/ ./cmd/...
./bin/quarry-index --corpus data/msmarco/collection.tsv --out data/indexes/msmarco
./bin/quarry-eval --index data/indexes/msmarco --queries data/msmarco/queries.dev.small.tsv \
  --qrels data/msmarco/qrels.dev.small.tsv --run runs/msmarco-dev.trec
```

Output is tab-separated, with metrics printed to four decimal places:

```text
queries	<count>
nDCG@10	<value>
R@100	<value>
R@1000	<value>
MRR@10	<value>
latency_ms_mean	<value>
latency_ms_p50	<value>
latency_ms_p95	<value>
latency_ms_p99	<value>
```

Latency is measured per query around the search call only, excluding analysis and run writing.
The latency lines report milliseconds to three decimal places, with nearest-rank percentiles;
an evaluation with no queries reports zero latency.

Compare all three algorithms on a saved index with:

```sh
scripts/bench-search.sh <index dir> <queries> <qrels>
```

The script builds `quarry-eval`, warms up each algorithm and result limit, then prints MRR and
latency statistics. It saves measured runs under `data/bench/search-<algo>-k<k>.trec` and checks
that both WAND and BMW run files are byte-identical to exhaustive search.

Metrics are computed like `trec_eval` from the saved run, with score ties broken by
document ID descending and averages over all judged queries, including those with no results.

## Results

BM25 (k1=0.9, b=0.4), title and text indexed as one field, test split, 1000 results per query.
Baselines are Anserini's published
[BM25 flat regressions](https://github.com/castorini/anserini/tree/master/src/main/resources/reproduce/from-document-collection/configs).

| Dataset  | Metric  | quarry | Anserini |
|----------|---------|-------:|---------:|
| SciFact  | nDCG@10 | 0.6777 | 0.6789   |
| SciFact  | R@100   | 0.9253 | 0.9253   |
| SciFact  | R@1000  | 0.9767 | 0.9767   |
| NFCorpus | nDCG@10 | 0.3201 | 0.3218   |
| NFCorpus | R@100   | 0.2456 | 0.2457   |
| NFCorpus | R@1000  | 0.3702 | 0.3704   |

MS MARCO passage ranking, dev small queries (6,980), BM25 (k1=0.9, b=0.4), 1000 results per query.
The baseline is Anserini's
[msmarco-v1-passage regression](https://github.com/castorini/anserini/blob/master/src/main/resources/reproduce/from-document-collection/configs/msmarco-v1-passage.yaml)
with default parameters. MRR@10 is `trec_eval -c -M 10 -m recip_rank`, which our metrics match exactly.

| Dataset  | Metric  | quarry | Anserini |
|----------|---------|-------:|---------:|
| MS MARCO | MRR@10  | 0.1843 | 0.1840   |
| MS MARCO | R@100   | 0.6590 | 0.6578   |
| MS MARCO | R@1000  | 0.8526 | 0.8526   |

The index holds 352,316,036 tokens in total, exactly the count Anserini reports for its index.

Text is analyzed like Anserini's English analyzer (Lucene tokenization, possessive removal,
lowercasing, stopwords, Porter stemming), verified token by token against real Lucene. The
remaining nDCG@10 gap most likely comes from Lucene's rounded document lengths. See
[docs/DESIGN.md](docs/DESIGN.md) for details and every known difference.

Our metrics match NIST [trec_eval](https://github.com/usnistgov/trec_eval) exactly on the SciFact run.
To check it yourself, build trec_eval and run:

```sh
tail -n +2 data/beir/scifact/qrels/test.tsv | awk -F'\t' '{print $1" 0 "$2" "$3}' > runs/scifact.qrels
trec_eval -c -m ndcg_cut.10 runs/scifact.qrels runs/scifact.trec
```

### Query latency and throughput

```sh
go build -o bin/ ./cmd/quarry-bench && ./bin/quarry-bench --index data/indexes/msmarco --queries data/msmarco/queries.dev.small.tsv
```

`quarry-bench` measures search only, excluding query analysis, and records CPU, core count,
memory, OS, and Go version. With one client it reports single-threaded query latency; with
multiple concurrent clients it reports throughput (QPS) and latency under load. All clients
share one saved index. The TSV table includes mean and nearest-rank p50, p95, and p99 latency.

Defaults compare `--algos exhaustive,wand,bmw`, `--k 10,1000`, and `--clients 1,2,4,8`.
Each algorithm and k gets one single-client warm-up pass before measurement for a warm OS page
cache. Set `--warmup 0` to skip priming, or increase it for more passes. `--limit N` selects the
first N queries in sorted query ID order; the default, 0, uses all queries.

RESULTS: TO BE FILLED IN

### MS MARCO index

Full MS MARCO passage corpus, built with the commands in [Evaluation](#evaluation) (default 1 GB
budget, 8 workers) on an Apple M3 (4 performance and 4 efficiency cores, 16 GB RAM, macOS):

| Measure | Value |
|---------|------:|
| Documents | 8,841,823 |
| Distinct terms | 2,660,824 |
| Postings | 266,247,718 |
| Build time | 83 s (about 106,000 documents per second, 11 temporary segments) |
| Index size | 885 MB (postings file 629 MB) |
| Bits per posting | 18.91 (document ID gap and term frequency, both varints) |
| Peak memory | 0.79 GB footprint (1.5 GB resident, see below) within the 1 GB budget |
| Evaluation, 6,980 queries | 193 s with exhaustive search (about 28 ms per query) |

Memory is the peak physical footprint reported by `/usr/bin/time -l` on macOS, the figure Activity
Monitor shows. macOS keeps counting memory that Go has already returned in the resident set size
until it needs the pages back, so RSS overstates use there.

Peak footprint by budget on MS MARCO (same machine; the index is byte-identical in every case):

| `--mem-budget` | Before budget-aware builds | Now | Build time now | Segments now |
|---------------:|---------------------------:|----:|---------------:|-------------:|
| 256 MB | 0.75 GB | 0.29 GB | 88 s | 44 |
| 512 MB | 1.52 GB | 0.44 GB | 90 s | 21 |
| 1 GB   | 2.79 GB | 0.79 GB | 83 s | 11 |
| 2 GB   | 5.70 GB | 1.43 GB | 83 s | 5 |

### Indexing speed

`scripts/bench-workers.sh` builds an index from SciFact repeated 20 times (103,660 documents,
162 MB) and reports the median wall time of 3 runs for each worker count. Measured on an Apple M3
(4 performance and 4 efficiency cores, 16 GB RAM, macOS), default budget, warm page cache:

| Workers | Seconds | Speedup vs. sequential | Peak memory |
|--------:|--------:|-----------------------:|------------:|
| sequential (before `--workers`) | 10.5 | 1.0x | 243 MB |
| 1 | 8.94 | 1.2x | 247 MB |
| 2 | 4.69 | 2.2x | 240 MB |
| 3 | 3.58 | 2.9x | 247 MB |
| 4 | 3.11 | 3.4x | 245 MB |
| 6 | 2.94 | 3.6x | 246 MB |
| 8 | 2.51 | 4.2x | 250 MB |

Before this change, analysis took about 78% of the build time; JSON decoding (about 3%) and adding
postings (about 18%) still run on one goroutine each, which limits the speedup to roughly 4.4x.

## Development

```sh
make build   # compile everything into bin/
make test    # run tests with the race detector
make lint    # go vet and golangci-lint
make check   # all of the above, same as CI
```

The analyzer's expected outputs in `internal/analysis/testdata/golden_lucene.txt` come from real
Lucene. After editing `golden_inputs.txt`, regenerate them with `scripts/lucene-golden.sh`
(needs Java 11 or newer; downloads Lucene into the ignored `data/` folder).
