package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStdout string
		wantErr    bool
	}{
		{name: "version flag", args: []string{"--version"}, wantStdout: "quarry-search dev\n"},
		{name: "help flag", args: []string{"-h"}},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "no arguments", args: nil, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)

			if (err != nil) != tt.wantErr {
				t.Fatalf("run(%q) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, got, tt.wantStdout)
			}
		})
	}
}
