package corpus

import (
	"errors"
	"os"
	"path/filepath"
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

// TestRead checks document order, analysis, and unchanged decoding errors.
func TestRead(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantIDs   []string
		wantTerms [][]string
		wantErr   string
	}{
		{name: "empty", input: " \n\t"},
		{name: "records", input: `{"_id":"a","title":"RED","text":"Fish fish!","metadata":{}} {"_id":"b"}`, wantIDs: []string{"a", "b"}, wantTerms: [][]string{{"red", "fish", "fish"}, nil}},
		{name: "missing id", input: `{"text":"fish"}`, wantErr: "record 1: _id is empty"},
		{name: "empty id", input: `{"_id":""}`, wantErr: "record 1: _id is empty"},
		{name: "malformed", input: `{"_id":"a"} {`, wantErr: "record 2:"},
		{name: "wrong type", input: `{"_id":42}`, wantErr: "record 1:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			var terms [][]string
			err := Read(strings.NewReader(tt.input), func(id string, analyzed []string) error {
				ids = append(ids, id)
				terms = append(terms, analyzed)
				return nil
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Read() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(ids, tt.wantIDs) {
				t.Fatalf("Read() IDs = %v, error = %v", ids, err)
			}
			for i := range terms {
				if !slices.Equal(terms[i], tt.wantTerms[i]) {
					t.Errorf("record %d terms = %v, want %v", i+1, terms[i], tt.wantTerms[i])
				}
			}
		})
	}
}

type recordReader struct {
	reads int
}

// Read supplies one record per call so a visit failure can detect further reads.
func (r *recordReader) Read(data []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		return 0, errors.New("unexpected read")
	}
	return copy(data, `{"_id":"a"}`+"\n"), nil
}

// TestReadVisitError checks that a visitor error stops input and remains wrapped.
func TestReadVisitError(t *testing.T) {
	reader := &recordReader{}
	want := errors.New("visitor stopped")
	calls := 0
	err := Read(reader, func(string, []string) error {
		calls++
		return want
	})
	if !errors.Is(err, want) || err.Error() != "record 1: visitor stopped" || calls != 1 || reader.reads != 1 {
		t.Fatalf("Read() error = %v, calls = %d, reads = %d", err, calls, reader.reads)
	}
}

// TestReadFile checks file and record error wrapping, including visitor errors.
func TestReadFile(t *testing.T) {
	visitErr := errors.New("visitor stopped")
	for _, tt := range []struct {
		name     string
		input    string
		missing  bool
		visitErr error
		wantErr  string
	}{
		{name: "valid", input: `{"_id":"a"}`},
		{name: "missing", missing: true, wantErr: "open corpus:"},
		{name: "invalid", input: "{", wantErr: "load corpus"},
		{name: "visitor", input: `{"_id":"a"}`, visitErr: visitErr, wantErr: "record 1: visitor stopped"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corpus.jsonl")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.input), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			err := ReadFile(path, func(string, []string) error { calls++; return tt.visitErr })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadFile() error = %v, want %q", err, tt.wantErr)
				}
				if tt.visitErr != nil && !errors.Is(err, tt.visitErr) {
					t.Fatalf("ReadFile() lost visitor error: %v", err)
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("ReadFile() error = %v, calls = %d", err, calls)
			}
		})
	}
}
