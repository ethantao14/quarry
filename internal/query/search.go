package query

import (
	"errors"

	"github.com/ethantao14/quarry/internal/scoring"
)

// CheckAlgorithm reports whether algo names a retrieval algorithm Search accepts.
func CheckAlgorithm(algo string) error {
	if algo != "exhaustive" && algo != "wand" {
		return errors.New("--algo must be exhaustive or wand")
	}
	return nil
}

// Search runs the selected retrieval algorithm.
func Search(algo string, ix Index, bm25 scoring.BM25, terms []string, k int) ([]Result, error) {
	if err := CheckAlgorithm(algo); err != nil {
		return nil, err
	}
	if algo == "wand" {
		return WAND(ix, bm25, terms, k)
	}
	return Exhaustive(ix, bm25, terms, k)
}
