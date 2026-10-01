# quarry

A full-text search engine written from scratch in Go. It builds an inverted index on disk, ranks
results with BM25, and speeds up top-k retrieval with dynamic pruning (WAND and Block-Max WAND).
It is evaluated on the public MS MARCO and BEIR benchmarks against published BM25 baselines.

**Status:** in-memory search with English text analysis and evaluation work. On-disk indexing and
pruning are not implemented yet.

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

## Evaluation

```sh
scripts/download.sh scifact
go run ./cmd/quarry-eval --dataset data/beir/scifact --run runs/scifact.trec --k 1000
```

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

| Dataset | Metric  | quarry | Anserini |
|---------|---------|-------:|---------:|
| SciFact | nDCG@10 | 0.6777 | 0.6789   |
| SciFact | R@100   | 0.9253 | 0.9253   |
| SciFact | R@1000  | 0.9767 | 0.9767   |

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
