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
