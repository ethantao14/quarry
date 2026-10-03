package corpus

import (
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadQueries(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{
			name:  "queries with unknown fields",
			input: "{\"_id\":\"q1\",\"text\":\"RED fish?\",\"metadata\":{\"source\":\"test\"}}\n{\"_id\":\"q2\",\"text\":\"Café\"}\n",
			want:  map[string]string{"q1": "RED fish?", "q2": "Café"},
		},
		{name: "empty input", want: map[string]string{}},
		{name: "whitespace input", input: " \n\t", want: map[string]string{}},
		{name: "empty text", input: `{"_id":"q1","text":""}`, want: map[string]string{"q1": ""}},
		{name: "missing text", input: `{"_id":"q1"}`, want: map[string]string{"q1": ""}},
		{name: "missing id", input: `{"text":"fish"}`, wantErr: "record 1: _id is empty"},
		{name: "empty id", input: `{"_id":"","text":"fish"}`, wantErr: "record 1: _id is empty"},
		{name: "malformed JSON", input: "{\"_id\":\"q1\"}\n{\"_id\":", wantErr: "record 2:"},
		{name: "empty second id", input: "{\"_id\":\"q1\"}\n{\"text\":\"fish\"}", wantErr: "record 2: _id is empty"},
		{name: "wrong id type", input: `{"_id":42}`, wantErr: "record 1:"},
		{name: "wrong text type", input: `{"_id":"q1","text":42}`, wantErr: "record 1:"},
		{name: "null record", input: "null", wantErr: "record 1: _id is empty"},
		{name: "trailing malformed JSON", input: `{"_id":"q1"} garbage`, wantErr: "record 2:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadQueries(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadQueries(%q) error = %v, want %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadQueries(%q) error = %v, want nil", tt.input, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ReadQueries(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadQueries(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		missing bool
		want    map[string]string
		wantErr string
	}{
		{name: "valid file", input: `{"_id":"q1","text":"fish"}`, want: map[string]string{"q1": "fish"}},
		{name: "missing file", missing: true, wantErr: "open queries:"},
		{name: "malformed file", input: "{", wantErr: "record 1:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queries.jsonl")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.input), 0o600); err != nil {
					t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
				}
			}
			got, err := LoadQueries(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadQueries(%q) error = %v, want %q", path, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadQueries(%q) error = %v, want nil", path, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("LoadQueries(%q) = %v, want %v", path, got, tt.want)
			}
		})
	}
}

func TestReadQueriesTSV(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{name: "empty", want: map[string]string{}},
		{name: "good lines", input: "q1\tRED fish?\nq2\tCafé\n", want: map[string]string{"q1": "RED fish?", "q2": "Café"}},
		{name: "tabs preserved", input: "q1\tfish\tblue\t\n", want: map[string]string{"q1": "fish\tblue\t"}},
		{name: "CRLF", input: "q1\tfish\r\n", want: map[string]string{"q1": "fish"}},
		{name: "no final newline", input: "q1\tfish", want: map[string]string{"q1": "fish"}},
		{name: "blank lines", input: "\n\r\nq1\tfish\n\n", want: map[string]string{"q1": "fish"}},
		{name: "empty text", input: "q1\t", want: map[string]string{"q1": ""}},
		{name: "duplicate id", input: "q1\tfirst\nq1\tlast\n", want: map[string]string{"q1": "last"}},
		{name: "strip one CR", input: "q1\tfish\r\r\n", want: map[string]string{"q1": "fish\r"}},
		{name: "missing tab", input: "q1 fish", wantErr: "line 1: missing tab"},
		{name: "empty id", input: "\tfish", wantErr: "line 1: id is empty"},
		{name: "invalid UTF-8", input: "q1\t\xff", wantErr: "line 1: invalid UTF-8"},
		{name: "invalid UTF-8 id", input: "\xff\tfish", wantErr: "line 1: invalid UTF-8"},
		{name: "line number", input: "q1\tfish\n\ninvalid", wantErr: "line 3: missing tab"},
		{name: "long line", input: "q1\t" + strings.Repeat("fish ", 250000), want: map[string]string{"q1": strings.Repeat("fish ", 250000)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, format := range []string{"reader", ".tsv", ".TSV"} {
				t.Run(format, func(t *testing.T) {
					var got map[string]string
					var err error
					var path string
					if format == "reader" {
						got, err = ReadQueriesTSV(strings.NewReader(tt.input))
					} else {
						path = filepath.Join(t.TempDir(), "queries"+format)
						if err := os.WriteFile(path, []byte(tt.input), 0o600); err != nil {
							t.Fatal(err)
						}
						got, err = LoadQueries(path)
					}
					if tt.wantErr != "" {
						want := tt.wantErr
						if path != "" {
							want = "load queries " + path + ": " + want
						}
						if err == nil || err.Error() != want {
							t.Fatalf("error = %v, want %q", err, want)
						}
						return
					}
					if err != nil || !maps.Equal(got, tt.want) {
						t.Fatalf("queries differ, error = %v", err)
					}
				})
			}
		})
	}
}

func TestReadQueriesTSVReadError(t *testing.T) {
	reader := io.MultiReader(strings.NewReader("q1\tfish\n\n"), iotest.ErrReader(io.ErrUnexpectedEOF))
	_, err := ReadQueriesTSV(reader)
	if !errors.Is(err, io.ErrUnexpectedEOF) || err.Error() != "line 3: unexpected EOF" {
		t.Fatalf("ReadQueriesTSV() error = %v", err)
	}
}
