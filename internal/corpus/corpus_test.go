package corpus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethantao14/quarry/internal/analysis"
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
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var ids []string
					var terms [][]string
					err := Read(strings.NewReader(tt.input), workers, func(id string, analyzed []string) error {
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
		})
	}
}

type recordReader struct {
	reads int
}

// Read supplies one record, then a read error to check visitor error precedence.
func (r *recordReader) Read(data []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		return 0, errors.New("unexpected read")
	}
	return copy(data, `{"_id":"a"}`+"\n"), nil
}

// TestReadVisitError checks that a visitor error wins over a later read error.
func TestReadVisitError(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			reader := &recordReader{}
			want := errors.New("visitor stopped")
			calls := 0
			err := Read(reader, workers, func(string, []string) error {
				calls++
				return want
			})
			if !errors.Is(err, want) || err.Error() != "record 1: visitor stopped" || calls != 1 {
				t.Fatalf("Read() error = %v, calls = %d", err, calls)
			}
		})
	}
}

// TestReadFile checks file and record error wrapping, including visitor errors.
func TestReadFile(t *testing.T) {
	visitErr := errors.New("visitor stopped")
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
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
					err := ReadFile(path, workers, func(string, []string) error {
						calls++
						return tt.visitErr
					})
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
		})
	}
}

func TestReadMatchesSequential(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	vocabulary := []string{"RED", "Blue", "Fish!", "river,", "the", "and", "running", "runs", "quiet", "stones."}
	randomText := func(words int) string {
		var text strings.Builder
		for range words {
			text.WriteString(vocabulary[rng.IntN(len(vocabulary))])
			text.WriteByte(' ')
		}
		return text.String()
	}
	for corpusNumber := range 50 {
		t.Run(fmt.Sprintf("corpus=%d", corpusNumber), func(t *testing.T) {
			records := rng.IntN(201)
			var input strings.Builder
			encoder := json.NewEncoder(&input)
			for i := range records {
				record := map[string]string{
					"_id":   fmt.Sprintf("d%d", i),
					"title": randomText(rng.IntN(10)),
					"text":  randomText(rng.IntN(100)),
				}
				if err := encoder.Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			type document struct {
				id    string
				terms []string
			}
			var expected []document
			decoder := json.NewDecoder(strings.NewReader(input.String()))
			for {
				var record struct {
					ID    string `json:"_id"`
					Title string `json:"title"`
					Text  string `json:"text"`
				}
				err := decoder.Decode(&record)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				expected = append(expected, document{record.ID, analysis.Analyze(record.Title + " " + record.Text)})
			}
			for _, workers := range []int{1, 2, 3, 8, records + 5} {
				t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
					var got []document
					err := Read(strings.NewReader(input.String()), workers, func(id string, terms []string) error {
						got = append(got, document{id, terms})
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if len(got) != len(expected) {
						t.Fatalf("visits = %d, want %d", len(got), len(expected))
					}
					for i, want := range expected {
						if got[i].id != want.id || !slices.Equal(got[i].terms, want.terms) {
							t.Errorf("record %d = %v, want %v", i+1, got[i], want)
						}
					}
				})
			}
		})
	}
}

func TestReadErrorsKeepOrder(t *testing.T) {
	var input strings.Builder
	for i := range 100 {
		fmt.Fprintf(&input, "{\"_id\":\"d%d\",\"text\":\"Running by the river.\"}\n", i)
	}
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			t.Run("decode", func(t *testing.T) {
				calls := 0
				err := Read(strings.NewReader(input.String()+"{"), workers, func(string, []string) error {
					calls++
					return nil
				})
				if calls != 100 || err == nil || !strings.Contains(err.Error(), "record 101:") {
					t.Fatalf("Read() error = %v, calls = %d, want record 101 error after 100 calls", err, calls)
				}
			})
			t.Run("visit", func(t *testing.T) {
				want := errors.New("visitor stopped")
				calls := 0
				err := Read(strings.NewReader(input.String()), workers, func(string, []string) error {
					calls++
					if calls == 37 {
						return want
					}
					return nil
				})
				if calls != 37 || !errors.Is(err, want) || !strings.HasPrefix(err.Error(), "record 37: ") {
					t.Fatalf("Read() error = %v, calls = %d, want visitor error after 37 calls", err, calls)
				}
			})
		})
	}
}

func TestReadStopsGoroutines(t *testing.T) {
	var input strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&input, "{\"_id\":\"d%d\",\"text\":\"Running by the river.\"}\n", i)
	}
	for _, stopAt := range []int{3, 0} {
		t.Run(fmt.Sprintf("stop=%d", stopAt), func(t *testing.T) {
			before := runtime.NumGoroutine()
			want := errors.New("visitor stopped")
			calls := 0
			err := Read(strings.NewReader(input.String()), 8, func(string, []string) error {
				calls++
				if calls == stopAt {
					return want
				}
				return nil
			})
			if stopAt == 0 {
				if err != nil || calls != 1000 {
					t.Fatalf("Read() error = %v, calls = %d, want success after 1000 calls", err, calls)
				}
			} else if !errors.Is(err, want) || calls != stopAt {
				t.Fatalf("Read() error = %v, calls = %d, want visitor error after %d calls", err, calls, stopAt)
			}
			deadline := time.Now().Add(time.Second)
			for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if after := runtime.NumGoroutine(); after > before {
				t.Fatalf("goroutines after Read = %d, want at most %d", after, before)
			}
		})
	}
}

func TestReadRejectsWorkers(t *testing.T) {
	for _, workers := range []int{0, -1} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			reader := &recordReader{}
			calls := 0
			err := Read(reader, workers, func(string, []string) error {
				calls++
				return nil
			})
			if err == nil || err.Error() != "workers must be at least 1" || calls != 0 || reader.reads != 0 {
				t.Fatalf("Read() error = %v, calls = %d, reads = %d", err, calls, reader.reads)
			}
		})
	}
}

func BenchmarkRead(b *testing.B) {
	subjects := strings.Fields("researchers teachers students farmers workers artists writers readers travelers visitors scientists engineers builders gardeners doctors nurses drivers sailors pilots cooks bakers musicians painters dancers runners cyclists hikers climbers swimmers families neighbors friends children parents volunteers guides managers planners designers explorers")
	verbs := strings.Fields("study examine observe explore describe discuss inspect consider discover record compare review measure test watch follow search find collect gather carry share prepare create build develop improve repair arrange move select identify explain remember notice organize present evaluate photograph sketch")
	adjectives := strings.Fields("small large bright quiet busy careful curious patient friendly skilled experienced young local distant ancient modern wooden green golden silver natural useful unusual familiar complex simple detailed colorful beautiful interesting important peaceful gentle strong soft smooth rough warm cool fresh")
	objects := strings.Fields("gardens forests rivers mountains valleys fields bridges houses towers roads paths trails boats trains bicycles baskets tools books maps letters reports drawings paintings photographs sculptures flowers trees plants stones shells seeds leaves branches samples buildings machines instruments tables chairs")
	locations := strings.Fields("village city town harbor coast beach island valley mountain forest garden park museum library school university hospital station market square farm meadow river lake stream bridge road trail workshop studio kitchen laboratory office warehouse theater gallery courtyard orchard vineyard greenhouse")
	rng := rand.New(rand.NewPCG(1, 2))
	word := func(vocabulary []string) string {
		return vocabulary[rng.IntN(len(vocabulary))]
	}
	var input strings.Builder
	encoder := json.NewEncoder(&input)
	for i := range 2000 {
		var text strings.Builder
		for range 10 {
			fmt.Fprintf(&text, "%s %s %s the %s %s near the %s, and %s %s %s before evening. ",
				word(adjectives), word(subjects), word(verbs), word(adjectives), word(objects),
				word(locations), word(verbs), word(adjectives), word(objects))
		}
		record := map[string]string{"_id": fmt.Sprintf("d%d", i), "title": "Daily field observations", "text": text.String()}
		if err := encoder.Encode(record); err != nil {
			b.Fatal(err)
		}
	}
	data := input.String()
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				err := Read(strings.NewReader(data), workers, func(string, []string) error {
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
