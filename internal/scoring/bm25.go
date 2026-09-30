// Package scoring computes how relevant a document is to a query term.
package scoring

import "math"

// BM25 holds the two BM25 tuning parameters.
// K1 controls how quickly repeated terms stop adding score; B controls how
// much longer documents are penalized.
type BM25 struct {
	K1 float64
	B  float64
}

// DefaultBM25 returns Anserini's default parameters.
func DefaultBM25() BM25 {
	return BM25{K1: 0.9, B: 0.4}
}

// IDF is Lucene's BM25 inverse document frequency. Rarer terms score higher.
func IDF(docCount, docFreq int) float64 {
	n := float64(docCount)
	df := float64(docFreq)
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

// TermScore is one term's BM25 contribution to a document's score, in the
// Lucene 8+ form that omits the constant (K1 + 1) factor.
func (p BM25) TermScore(idf float64, tf, docLen uint32, avgDocLen float64) float64 {
	freq := float64(tf)
	lengthNorm := p.K1 * (1 - p.B + p.B*float64(docLen)/avgDocLen)
	return idf * freq / (freq + lengthNorm)
}
