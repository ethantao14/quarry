# quarry

A full-text search engine written from scratch in Go. It builds an inverted index on disk, ranks
results with BM25, and speeds up top-k retrieval with dynamic pruning (WAND and Block-Max WAND).
It is evaluated on the public MS MARCO and BEIR benchmarks against published BM25 baselines.

**Status:** English text analysis, BM25 search, evaluation, and saving an index to disk (read back
with memory mapping) work. Indexing at MS MARCO scale and pruning are not implemented yet.

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

Use a BEIR JSONL corpus with `_id`, `title`, and `text` fields. Results are tab-separated:
`rank` (1-based), `external document ID`, and `score` (four decimal places).

To analyze the corpus once instead of on every search, save an index and search that:

```sh
go build -o bin/ ./cmd/...
./bin/quarry-index --corpus cmd/quarry-search/testdata/tiny.jsonl --out data/indexes/tiny
./bin/quarry-search --index data/indexes/tiny --k 2 fish
```

`quarry-index` prints the document count, distinct term count, number of temporary segments, and
total index size in bytes. It refuses to overwrite an existing directory, and removes its partial
output if indexing fails.

To index a corpus larger than memory, cap the memory used for collecting postings with
`--mem-budget` (bytes, or a number with `KB`, `MB`, or `GB`; default `1GB`). When the budget is
reached, quarry writes what it has as a temporary segment and continues; at the end it merges the
segments into one index. The result is byte-for-byte the same for any budget. The budget is an
estimate of postings memory, not a hard limit on the whole process (see
[docs/DESIGN.md](docs/DESIGN.md#indexing-within-a-memory-budget)).

Text analysis runs on `--workers` goroutines (default: the number of CPUs Go uses). The index is
byte-for-byte the same for any worker count. Use exactly one of `--corpus` or `--index` when
searching. Results from a saved index are identical to searching the corpus directly. Saved indexes
are memory-mapped, which works on macOS and Linux. The format is
described in [docs/DESIGN.md](docs/DESIGN.md#on-disk-index-format).

## Evaluation

```sh
scripts/download.sh scifact
go run ./cmd/quarry-eval --dataset data/beir/scifact --run runs/scifact.trec --k 1000
```

The same works for NFCorpus: replace `scifact` with `nfcorpus` in both commands. To evaluate a
saved index, add `--index <dir>`; queries and judgments still come from `--dataset`.

Output is tab-separated, with metrics printed to four decimal places:

```text
queries	<count>
nDCG@10	<value>
R@100	<value>
R@1000	<value>
MRR@10	<value>
```

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
