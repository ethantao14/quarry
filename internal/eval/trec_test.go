package eval

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadQrels(t *testing.T) {
	const header = "query-id\tcorpus-id\tscore\n"
	tests := []struct {
		name    string
		input   string
		want    Qrels
		wantErr string
	}{
		{name: "grades and queries", input: header + "q1\ta\t2\nq2\tb\t0\nq1\tc\t-1\n", want: Qrels{"q1": {"a": 2, "c": -1}, "q2": {"b": 0}}},
		{name: "blank lines and no final newline", input: header + "\nq1\ta\t1", want: Qrels{"q1": {"a": 1}}},
		{name: "CRLF", input: "query-id\tcorpus-id\tscore\r\nq1\ta\t1\r\n", want: Qrels{"q1": {"a": 1}}},
		{name: "header only", input: header, want: Qrels{}},
		{name: "empty input", wantErr: "line 1: missing qrels header"},
		{name: "missing header", input: "q1\ta\t1", wantErr: "line 1: expected 4 fields, got 3"},
		{name: "wrong header", input: "query-id corpus-id score\n", wantErr: "line 1: expected 4 fields, got 3"},
		{name: "blank first line", input: "\n" + header, want: Qrels{}},
		{name: "too few fields", input: header + "q1\ta", wantErr: "line 2: expected 3"},
		{name: "too many fields", input: header + "q1\ta\t1\textra", wantErr: "line 2: expected 3"},
		{name: "spaces instead of tabs", input: header + "q1 a 1", wantErr: "line 2: expected 3"},
		{name: "bad grade", input: header + "q1\ta\tbad", wantErr: "line 2: invalid grade"},
		{name: "fractional grade", input: header + "q1\ta\t1.5", wantErr: "line 2: invalid grade"},
		{name: "line number includes blanks", input: header + "\nq1\ta\t1\nq2\tb\tx", wantErr: "line 4: invalid grade"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadQrels(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadQrels(%q) error = %v, want %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadQrels(%q) error = %v, want nil", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadQrels(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestReadRun(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Run
		wantErr string
	}{
		{name: "empty input", want: Run{}},
		{name: "blank lines", input: "\n \t\r\n", want: Run{}},
		{
			name:  "whitespace and ignored columns",
			input: "q1 anything b ignored 1.25 tag\n\n q2\tQ0\ta\t7\t-2e-1\tx\r\nq1 Q0 c 1 3 other",
			want:  Run{"q1": {{DocID: "b", Score: 1.25}, {DocID: "c", Score: 3}}, "q2": {{DocID: "a", Score: -0.2}}},
		},
		{name: "too few fields", input: "q1 Q0 a 1 1.0", wantErr: "line 1: expected 6"},
		{name: "too many fields", input: "q1 Q0 a 1 1.0 tag extra", wantErr: "line 1: expected 6"},
		{name: "bad score", input: "q1 Q0 a 1 bad tag", wantErr: "line 1: invalid score"},
		{name: "score overflow", input: "q1 Q0 a 1 1e999 tag", wantErr: "line 1: invalid score"},
		{name: "line number includes blanks", input: "q1 Q0 a 1 1 tag\n\nq2 Q0 b 1 bad tag", wantErr: "line 3: invalid score"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadRun(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadRun(%q) error = %v, want %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadRun(%q) error = %v, want nil", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadRun(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestWriteRunRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		entries []RunEntry
		want    string
		wantRun Run
	}{
		{name: "empty entries", wantRun: Run{}},
		{
			name:    "preserves order and rounds scores",
			entries: []RunEntry{{DocID: "b", Score: 1.23456789}, {DocID: "a", Score: 2}, {DocID: "c", Score: -0.25}},
			want:    "q1 Q0 b 1 1.234568 quarry\nq1 Q0 a 2 2.000000 quarry\nq1 Q0 c 3 -0.250000 quarry\n",
			wantRun: Run{"q1": {{DocID: "b", Score: 1.234568}, {DocID: "a", Score: 2}, {DocID: "c", Score: -0.25}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteRun(&output, "q1", tt.entries, "quarry"); err != nil {
				t.Fatalf("WriteRun() error = %v, want nil", err)
			}
			if got := output.String(); got != tt.want {
				t.Errorf("WriteRun() output = %q, want %q", got, tt.want)
			}
			got, err := ReadRun(&output)
			if err != nil {
				t.Fatalf("ReadRun() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got, tt.wantRun) {
				t.Errorf("ReadRun() = %v, want %v", got, tt.wantRun)
			}
		})
	}
}

func TestTrecIOErrors(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		read    func(io.Reader) error
		wantErr string
	}{
		{
			name: "ReadRun",
			read: func(r io.Reader) error {
				_, err := ReadRun(r)
				return err
			},
			wantErr: "line 1:",
		},
		{
			name: "ReadQrels header",
			read: func(r io.Reader) error {
				_, err := ReadQrels(r)
				return err
			},
			wantErr: "line 1:",
		},
		{
			name:   "ReadQrels body",
			prefix: "query-id\tcorpus-id\tscore\n",
			read: func(r io.Reader) error {
				_, err := ReadQrels(r)
				return err
			},
			wantErr: "line 2:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := io.MultiReader(strings.NewReader(tt.prefix), iotest.ErrReader(io.ErrUnexpectedEOF))
			err := tt.read(r)
			if !errors.Is(err, io.ErrUnexpectedEOF) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s(reader error) = %v, want %q and %v", tt.name, err, tt.wantErr, io.ErrUnexpectedEOF)
			}
		})
	}
	t.Run("WriteRun", func(t *testing.T) {
		reader, writer := io.Pipe()
		if err := reader.Close(); err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}
		defer func() { _ = writer.Close() }()
		err := WriteRun(writer, "q1", []RunEntry{{DocID: "a", Score: 1}}, "quarry")
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("WriteRun(closed pipe) error = %v, want %v", err, io.ErrClosedPipe)
		}
	})
}

// FuzzReadRun checks that arbitrary run input does not panic.
func FuzzReadRun(f *testing.F) {
	for _, seed := range []string{"", "q1 Q0 a 1 1.234567 quarry\n", "\nq1\tQ0\tb\t2\t-1e2\ttag\r\n", "bad", "q1 Q0 a 1 bad tag"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ReadRun(strings.NewReader(input))
	})
}

// FuzzReadQrels checks that arbitrary qrels input does not panic.
func FuzzReadQrels(f *testing.F) {
	for _, seed := range []string{"", "query-id\tcorpus-id\tscore\n", "query-id\tcorpus-id\tscore\nq1\ta\t1\n\nq2\tb\t0\n", "query-id\tcorpus-id\tscore\nq1\ta\tbad", "bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ReadQrels(strings.NewReader(input))
	})
}

func TestReadQrelsTREC(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Qrels
		wantErr string
	}{
		{name: "good file", input: "q1 0 a 1\nq2 0 b 0\nq1 0 c -1\n", want: Qrels{"q1": {"a": 1, "c": -1}, "q2": {"b": 0}}},
		{name: "tabs and spaces", input: "q1\t0\ta\t2\r\n q2 ignored b 1", want: Qrels{"q1": {"a": 2}, "q2": {"b": 1}}},
		{name: "blank lines", input: "\n \t\nq1 0 a 1\n\n \t\n", want: Qrels{"q1": {"a": 1}}},
		{name: "duplicate judgment", input: "q1 0 a 1\nq1 0 a 2\n", want: Qrels{"q1": {"a": 2}}},
		{name: "three fields", input: "q1 a 1\n", wantErr: "line 1: expected 4 fields, got 3"},
		{name: "five fields", input: "q1 0 a 1 extra\n", wantErr: "line 1: expected 4 fields, got 5"},
		{name: "bad grade", input: "q1 0 a bad\n", wantErr: "line 1: invalid grade:"},
		{name: "fractional grade", input: "q1 0 a 1.5\n", wantErr: "line 1: invalid grade:"},
		{name: "line number", input: "\nq1 0 a 1\n\nq2 0 b bad", wantErr: "line 4: invalid grade:"},
		{name: "blank input", input: "\n \t\n", wantErr: "line 1: missing qrels"},
		{name: "BEIR after blanks", input: "\n \t\nquery-id\tcorpus-id\tscore\nq1\ta\t1", want: Qrels{"q1": {"a": 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadQrels(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadQrels() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ReadQrels() = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}
