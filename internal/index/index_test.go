package index

import (
	"slices"
	"strings"
	"testing"
)

func TestAdd(t *testing.T) {
	ix := New()
	docs := []struct {
		id   string
		text string
	}{
		{"a", "red fish blue fish"},
		{"b", "one fish"},
		{"c", "red car"},
	}
	for want, doc := range docs {
		got := ix.Add(doc.id, strings.Fields(doc.text))
		if got != uint32(want) {
			t.Fatalf("Add(%q) returned doc ID %d, want %d", doc.id, got, want)
		}
	}

	postingTests := []struct {
		term string
		want []Posting
	}{
		{"fish", []Posting{{DocID: 0, TF: 2}, {DocID: 1, TF: 1}}},
		{"red", []Posting{{DocID: 0, TF: 1}, {DocID: 2, TF: 1}}},
		{"car", []Posting{{DocID: 2, TF: 1}}},
		{"missing", nil},
	}
	for _, tt := range postingTests {
		if got := ix.Postings(tt.term); !slices.Equal(got, tt.want) {
			t.Errorf("Postings(%q) = %v, want %v", tt.term, got, tt.want)
		}
	}

	if got := ix.DocCount(); got != 3 {
		t.Errorf("DocCount() = %d, want 3", got)
	}
	if got := ix.DocLen(0); got != 4 {
		t.Errorf("DocLen(0) = %d, want 4", got)
	}
	if got, want := ix.AvgDocLen(), 8.0/3.0; got != want {
		t.Errorf("AvgDocLen() = %v, want %v", got, want)
	}
	if got := ix.ExternalID(2); got != "c" {
		t.Errorf("ExternalID(2) = %q, want %q", got, "c")
	}
}

func TestEmptyIndex(t *testing.T) {
	ix := New()
	if got := ix.DocCount(); got != 0 {
		t.Errorf("DocCount() = %d, want 0", got)
	}
	if got := ix.AvgDocLen(); got != 0 {
		t.Errorf("AvgDocLen() = %v, want 0", got)
	}
}
