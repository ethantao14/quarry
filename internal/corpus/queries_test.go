package corpus

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
