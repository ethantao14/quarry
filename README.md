# quarry

A full-text search engine written from scratch in Go. It builds an inverted index on disk, ranks
results with BM25, and speeds up top-k retrieval with dynamic pruning (WAND and Block-Max WAND).
It is evaluated on the public MS MARCO and BEIR benchmarks against published BM25 baselines.

**Status:** early development. Indexing and search are not implemented yet.

## Requirements

- Go 1.27.1 or newer
- [golangci-lint](https://golangci-lint.run/) v2 (only needed for `make lint`)

## Usage

```sh
git clone https://github.com/ethantao14/quarry.git
cd quarry
go run ./cmd/quarry-search --version
```

## Development

```sh
make build   # compile everything
make test    # run tests with the race detector
make lint    # go vet and golangci-lint
make check   # all of the above, same as CI
```
