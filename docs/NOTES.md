# Learning log

## M1: in-memory engine and correctness on BEIR

### What was built
- An English analyzer that reproduces Anserini's (Lucene 10.5): Unicode word splitting, possessive
  removal, lowercasing, 33 stopwords, and a from-scratch classic Porter stemmer.
- An in-memory inverted index: for every term, the documents containing it and how many times.
- BM25 scoring with Lucene's formulas, and exhaustive document-at-a-time top-k search with a min-heap.
- Evaluation: nDCG@k, Recall@k, MRR@k, TREC run files, and ranking rules identical to trec_eval.
- A Lucene "oracle": a Java helper that runs the real Lucene analyzer, used to generate test
  expectations and to compare our output token by token.

### What to understand deeply
1. **Why BM25 looks the way it does.** IDF rewards rare terms. The tf part saturates: the 10th
   occurrence of a word adds far less than the 2nd (k1 controls how fast). Length normalization
   stops long documents from winning just by being long (b controls how much).
2. **Document-at-a-time with a heap.** All query terms' postings lists are sorted by doc ID, so we
   walk them together like a k-way merge and score one document at a time. The heap keeps only the
   best k, so memory is O(k), not O(matching documents), and each candidate costs O(log k).
3. **Why analysis matters more than scoring.** Before stemming and stopwords, SciFact nDCG@10 was
   0.6618; after, 0.6777. The scoring code did not change. Recall@100 went from 0.8852 to 0.9253.
   Stemming lets "infections" match "infection"; stopword removal and stemming were added together,
   so their separate contributions were not measured.
4. **How to prove correctness against a reference.** Published numbers are the target, but the
   strongest evidence was matching Lucene token by token (838,126 of 838,128 terms on SciFact) and
   matching trec_eval's metrics exactly. The remaining 0.12-point nDCG gap has a known, verified
   cause: Lucene rounds document lengths above 40 to fit them in one byte.
5. **Test oracles and golden files.** Hand-written tricky inputs, with expected outputs produced by
   the real system (Lucene) rather than by us, so the test checks us against something independent.

### Likely interview questions
- *"Walk me through BM25. What do k1 and b do, and what happens at the extremes (b = 0, k1 = 0)?"*
  b = 0 ignores document length entirely; k1 = 0 makes term frequency irrelevant (any occurrence
  scores the same).
- *"Your library stemmer crashed on real data. How did you find it, and why did its tests pass?"*
  Its test vocabulary (Porter's 23,531 words) has no words made only of a suffix, like "eed".
  Running every token of the real corpora found it; then an exhaustive comparison of 69,904 short
  words against Lucene found 3 crashes and 2 wrong outputs.
- *"Your nDCG is 0.12 points below Anserini's but recall matches exactly. What does that tell you?"*
  The same documents are retrieved; only the order of a few near-ties differs. That points at score
  precision (Lucene's one-byte document lengths and 32-bit floats), not at analysis or retrieval.

### Key numbers
| Dataset  | Metric  | Before analyzer | quarry | Anserini |
|----------|---------|----------------:|-------:|---------:|
| SciFact  | nDCG@10 | 0.6618 | 0.6777 | 0.6789 |
| SciFact  | R@100   | 0.8852 | 0.9253 | 0.9253 |
| SciFact  | R@1000  | 0.9650 | 0.9767 | 0.9767 |
| NFCorpus | nDCG@10 | (not measured) | 0.3201 | 0.3218 |
| NFCorpus | R@100   | (not measured) | 0.2456 | 0.2457 |
| NFCorpus | R@1000  | (not measured) | 0.3702 | 0.3704 |

- Total terms after analysis: SciFact 838,126 (Lucene 838,128), NFCorpus 637,485 (exact).
- Rough end-to-end timings (load corpus, build index, run all test queries, write and score the
  run), median of 3 runs on an Apple M3 laptop, macOS 26.3: SciFact (5,183 docs, 300 queries)
  0.96 s and 45 MB peak memory; NFCorpus (3,633 docs, 323 queries) 0.55 s and 27 MB. Command:
  `make build && /usr/bin/time -l ./bin/quarry-eval --dataset data/beir/scifact --run runs/scifact.trec`.
  These are not benchmarks; M4 measures query latency properly.

## M2: on-disk segments and MS MARCO scale

### What was built
- An on-disk index format (version 1): a sorted, fixed-width term dictionary searched by binary search,
  postings as delta-encoded doc IDs and term frequencies in varints, document lengths, the
  internal-to-external ID map, and a manifest with sizes and CRC32C checksums. Every file has a magic
  number and format version, and a mismatch fails loudly.
- Reads through memory-mapped files, so opening an index costs almost nothing up front.
- SPIMI indexing: postings collect in memory until the chunk budget is reached, the chunk is written as
  a temporary segment, and a k-way merge combines the segments at the end. The output is byte-for-byte
  the same for any budget.
- Parallel text analysis with a worker pool that still delivers documents in input order.
- MS MARCO end to end: TSV input, TREC qrels, indexing statistics, and a memory budget that bounds the
  whole build.

### What to understand deeply
1. **Why delta + varint.** Postings are sorted by doc ID, so storing the gap between IDs gives small
   numbers, and varints spend one byte on numbers below 128. A posting costs 18.91 bits on MS MARCO
   instead of 64 for two raw uint32s. The decoder must treat its input as hostile: it is fuzzed and
   returns errors instead of panicking.
2. **What mmap buys and what it costs.** The operating system loads pages only when touched and keeps
   them in the page cache, shared across processes and runs. Opening a 10.7 MB index went from 3.1 ms
   and 10.7 MB of heap (read everything) to 1.75 ms and 5.4 KB. The cost: a page fault on first touch,
   and memory that the Go garbage collector cannot see or limit.
3. **Why SPIMI and a k-way merge.** Each chunk takes a contiguous range of doc IDs, so merged postings
   lists are just concatenated in segment order, with no re-sorting. A min-heap over the segments'
   sorted dictionaries yields every term once, in order. Sharing one streaming segment writer between
   the single-pass write and the merge is what makes the output identical for any budget.
4. **Ordered parallelism.** Each job carries its own one-slot result channel, and the consumer reads
   jobs in input order from a bounded queue. Workers finish in any order, but documents reach the
   builder in order, so doc IDs never depend on the worker count. The speedup is capped by what stays
   serial (decoding and adding postings): 4.2x at 8 workers against a predicted 4.4x ceiling.
5. **Memory accounting is subtle.** Three surprises: Go lets the heap grow to twice the live data
   unless given a soft limit; a substring keeps its whole parent string alive (stored doc IDs kept
   every MS MARCO passage in memory, 1.2 GB); and on macOS the resident set size keeps counting memory
   Go has already returned, so peak footprint is the honest number. Always measure; the profiler also
   misled once (it blamed read syscalls that made no difference to wall time).

### Likely interview questions
- *"How do you build an index bigger than memory?"* Collect postings for a chunk of documents, flush
  the chunk as a sorted segment when it reaches its budget, and merge the segments with a k-way merge
  over their sorted dictionaries. Doc IDs are assigned in order, so merging postings is concatenation.
- *"Why mmap instead of reading files?"* Startup cost and memory scale with what queries touch, not
  with index size, and the page cache is shared. The trade-off is less control: page faults on cold
  data, and memory that falls outside the language runtime's accounting.
- *"Your parallel indexer gets 4.2x on 8 cores. Why not 8x?"* Amdahl's law: JSON decoding and adding
  postings stay on one goroutine each (about 2.3 s of a 10.5 s build), which caps the speedup near
  4.4x. Sharding the accumulators would lift the cap at the cost of a merge step.

### Key numbers
| Dataset  | Metric | quarry | Anserini |
|----------|--------|-------:|---------:|
| MS MARCO (dev small) | MRR@10 | 0.1843 | 0.1840 |
| MS MARCO (dev small) | R@100  | 0.6590 | 0.6578 |
| MS MARCO (dev small) | R@1000 | 0.8526 | 0.8526 |

- MS MARCO index: 8,841,823 documents, 2,660,824 terms, 266,247,718 postings, 885 MB, 18.91 bits per
  posting, 352,316,036 total tokens (Anserini's count exactly). Built in 83 s at the default 1 GB budget
  with 8 workers, peak footprint 0.79 GB (Apple M3, 16 GB RAM, macOS).
- Before the budget bounded the whole build, the same 1 GB budget used 2.79 GB, and 2 GB used 5.70 GB.
- Disk index results are identical to the in-memory index on SciFact and NFCorpus, for budgets from
  16 KB (1,849 segments) to 1 GB.
- Postings decode at 5.4 to 5.8 ns per posting (`go test -run '^$' -bench Decode -count 3 ./internal/postings`).
- Analysis speedup on SciFact repeated 20 times: 1 worker 8.94 s, 8 workers 2.51 s, against 10.5 s
  before parallel analysis (`scripts/bench-workers.sh`).

## M3: dynamic pruning

### What was built
- Block format v2: 128-posting blocks with varint doc ID gaps and term frequencies inside each
  block. `seg0.skip` has 12-byte entries holding lastDoc, offset, and blockMax. The dictionary
  stores a per-term maxScore. Bounds are computed in float64 and rounded up to float32.
- Memory and disk cursors with `Next` and `Advance`; disk `Advance` binary-searches skip data
  and decodes only the destination block. Memory cursors lazily cache matching block bounds.
- WAND retrieval using per-term upper bounds and a top-k heap, plus Block-Max WAND (BMW) using
  shallow advance and tighter block bounds. Shallow advance moves only the block pointer,
  without decoding postings or changing the current posting.
- BMW as the default in search and evaluation, with `--algo wand` and `--algo exhaustive` available.
  Randomized tests compare exact results on memory and disk indexes; the search benchmark script
  compares all three algorithms and checks byte-identical runs.

### What to understand deeply
1. **WAND's pivot.** Sort cursors by current doc ID, then accumulate term bounds until their sum
   can beat the heap threshold. That cursor's doc ID is the pivot. Earlier docs cannot win.
   If all leading cursors reach the pivot, score it; otherwise advance a cursor toward it.
2. **BMW's tighter bound.** Include every cursor on the pivot doc, then shallow-advance the prefix
   to its blocks covering that doc and sum their block bounds. If the sum cannot win, skip toward
   the earliest block end plus one, capped by the first cursor outside the prefix. Before that
   boundary, only prefix terms can contribute, each within its current block. WAND already rules
   out docs before the pivot. Advancing one prefix cursor preserves every possible winner.
3. **Exact means exact.** Query terms have a stable lexical rank, and score contributions are
   summed in that rank order to match Exhaustive's float64 bits. Docs are scored in ascending ID
   order, so later docs must strictly beat the threshold because ties favor lower IDs. Bounds
   rounded upward and a conservative 1e-9 comparison margin protect against floating point
   summation differences. The margin can cause extra work, but cannot remove a winner.
4. **Pruning has overhead.** At k=1000 the heap takes longer to fill and its threshold stays lower.
   WAND may skip too little to repay cursor sorting, pivot selection, and repeated advances, so it
   can be slower than exhaustive search. BMW helps when block bounds are much tighter than term
   bounds; gains depend on the query and the distribution of scores across blocks.
5. **Bounds belong to a scoring model.** Stored bounds use the index's BM25 parameters and corpus
   statistics. Changing k1 or b can invalidate them. WAND and BMW reject mismatched parameters;
   exhaustive search can still score with another BM25 configuration.

### Likely interview questions
- *"Why can you skip a document without scoring it?"* A safe upper bound on all contributions is
  below the score needed to enter the full heap. The pivot argument excludes earlier docs, and
  BMW's block boundary limits how far the tighter bound remains valid.
- *"Why store a separate shallow pointer?"* Checking bounds needs only skip metadata. Keeping the
  posting position unchanged avoids decoding blocks until an actual advance needs their postings.
- *"How do you preserve exact results despite float32 bounds?"* Round bounds upward, use a small
  conservative comparison margin, score in the reference term order, and preserve the same tie rule.
- *"Why might a pruning algorithm lose to exhaustive search?"* Large k or loose bounds can leave
  nearly every candidate to score while adding sorting, branching, and cursor management costs.
- *"Can you tune BM25 at query time?"* Exhaustive search can; pruning needs bounds computed for the
  same parameters, or a rebuild with the new scoring model.

### Key numbers
KEY NUMBERS: TO BE FILLED IN
