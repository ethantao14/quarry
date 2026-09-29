package query

import (
	"math"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/index"
	"github.com/ethantao14/quarry/internal/scoring"
)

// Scores worked out by hand (k1=0.9, b=0.4, N=3, avgdl=10/3) and
// cross-checked with a separate script, not with this package.
func TestExhaustiveGolden(t *testing.T) {
	ix := index.New()
	ix.Add("d0", strings.Fields("the quick brown fox"))
	ix.Add("d1", strings.Fields("the lazy dog"))
	ix.Add("d2", strings.Fields("quick quick dog"))

	got := Exhaustive(ix, scoring.DefaultBM25(), []string{"quick", "dog"}, 10)
	want := []Result{
		{DocID: 2, Score: 0.5803626947},
		{DocID: 1, Score: 0.2521478698},
		{DocID: 0, Score: 0.2383385544},
	}
	if len(got) != len(want) {
		t.Fatalf("Exhaustive() returned %d results, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].DocID != want[i].DocID || math.Abs(got[i].Score-want[i].Score) > 1e-9 {
			t.Errorf("Exhaustive()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
