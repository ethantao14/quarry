package scoring

import (
	"math"
	"testing"
)

// Hand-computed for N=3, df=2: ln(1 + 1.5/2.5) = ln(1.6).
const idfTwoOfThree = 0.4700036292

func TestIDF(t *testing.T) {
	tests := []struct {
		name      string
		docCount  int
		docFreq   int
		wantScore float64
	}{
		{name: "two of three", docCount: 3, docFreq: 2, wantScore: idfTwoOfThree},
		{name: "in every doc stays positive", docCount: 3, docFreq: 3, wantScore: math.Log(1 + 0.5/3.5)},
		{name: "rare term", docCount: 1000, docFreq: 1, wantScore: math.Log(1 + 999.5/1.5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IDF(tt.docCount, tt.docFreq)
			if math.Abs(got-tt.wantScore) > 1e-9 {
				t.Errorf("IDF(%d, %d) = %.10f, want %.10f", tt.docCount, tt.docFreq, got, tt.wantScore)
			}
		})
	}
}

func TestTermScore(t *testing.T) {
	bm25 := DefaultBM25()
	avgDocLen := 10.0 / 3.0
	tests := []struct {
		name   string
		tf     uint32
		docLen uint32
		want   float64
	}{
		// norm = 0.9 * (0.6 + 0.4 * 4 / (10/3)) = 0.972
		{name: "tf 1, longer doc", tf: 1, docLen: 4, want: idfTwoOfThree / 1.972},
		// norm = 0.9 * (0.6 + 0.4 * 3 / (10/3)) = 0.864
		{name: "tf 1, shorter doc", tf: 1, docLen: 3, want: idfTwoOfThree / 1.864},
		{name: "tf 2, shorter doc", tf: 2, docLen: 3, want: idfTwoOfThree * 2 / 2.864},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bm25.TermScore(idfTwoOfThree, tt.tf, tt.docLen, avgDocLen)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("TermScore(tf=%d, docLen=%d) = %.10f, want %.10f", tt.tf, tt.docLen, got, tt.want)
			}
		})
	}
}
