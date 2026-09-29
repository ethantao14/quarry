# quarry

A full-text search engine written from scratch in Go. It builds an inverted index on disk, ranks
results with BM25, and speeds up top-k retrieval with dynamic pruning (WAND and Block-Max WAND).
It is evaluated on the public MS MARCO and BEIR benchmarks against published BM25 baselines.

**Status:** basic in-memory search works. On-disk indexing, evaluation, and pruning are not implemented yet.

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

## Development

```sh
make build   # compile everything into bin/
make test    # run tests with the race detector
make lint    # go vet and golangci-lint
make check   # all of the above, same as CI
```
