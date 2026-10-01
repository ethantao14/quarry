package corpus

import (
	"slices"
	"strings"
	"testing"

	"github.com/ethantao14/quarry/internal/postings"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIDs []string
		wantErr string
	}{
		{
			name: "good input",
			input: `{"_id":"a","title":"RED","text":"Fish fish!","metadata":{"source":"test"}}
{"_id":"b","title":"Blue","text":"fish"}`,
			wantIDs: []string{"a", "b"},
		},
		{name: "empty corpus", input: " \n\t"},
		{name: "malformed JSON", input: `{"_id":"a"}` + "\n" + `{"_id":`, wantErr: "record 2:"},
		{name: "missing _id", input: `{"text":"fish"}`, wantErr: "record 1: _id is empty"},
		{name: "empty _id", input: `{"_id":"","text":"fish"}`, wantErr: "record 1: _id is empty"},
		{name: "missing _id after valid record", input: `{"_id":"a"} {"text":"fish"}`, wantErr: "record 2: _id is empty"},
		{name: "wrong field type", input: `{"_id":42}`, wantErr: "record 1:"},
		{name: "trailing malformed JSON", input: `{"_id":"a"} garbage`, wantErr: "record 2:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix, err := Load(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load(%q) error = %v, want %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load(%q) error = %v, want nil", tt.input, err)
			}
			if got := ix.DocCount(); got != len(tt.wantIDs) {
				t.Fatalf("DocCount() = %d, want %d", got, len(tt.wantIDs))
			}
			for docID, want := range tt.wantIDs {
				if got := ix.ExternalID(uint32(docID)); got != want {
					t.Errorf("ExternalID(%d) = %q, want %q", docID, got, want)
				}
			}
		})
	}
}

func TestLoadTokenizesTitleAndText(t *testing.T) {
	ix, err := Load(strings.NewReader(`{"_id":"a","title":"RED","text":"Fish fish!"}`))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	tests := []struct {
		term string
		want []postings.Posting
	}{
		{term: "red", want: []postings.Posting{{DocID: 0, TF: 1}}},
		{term: "fish", want: []postings.Posting{{DocID: 0, TF: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			got, err := ix.Postings(tt.term)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Postings(%q) = %v, want %v", tt.term, got, tt.want)
			}
		})
	}
}
