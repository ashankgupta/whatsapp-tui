package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// emptyInput is an empty, non-terminal standard input.
func emptyInput(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseSendArgs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(file, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	req, err := parseSendArgs([]string{"Rahul", "running", "late"}, emptyInput(t))
	if err != nil || req.To != "Rahul" || req.Text != "running late" || req.File != "" {
		t.Fatalf("text: got %+v, %v", req, err)
	}

	req, err = parseSendArgs([]string{"me", "--file", file, "monthly", "report"}, emptyInput(t))
	if err != nil || req.To != "me" || req.File != file || req.Caption != "monthly report" || req.Text != "" {
		t.Fatalf("file: got %+v, %v", req, err)
	}

	req, err = parseSendArgs([]string{"-f", file, "Family"}, emptyInput(t))
	if err != nil || req.To != "Family" || req.File != file {
		t.Fatalf("file first: got %+v, %v", req, err)
	}

	if _, err := parseSendArgs([]string{"--help"}, emptyInput(t)); !errors.Is(err, errHelp) {
		t.Fatalf("help: got %v", err)
	}
	if _, err := parseSendArgs(nil, emptyInput(t)); err == nil {
		t.Fatal("no recipient: expected an error")
	}
	if _, err := parseSendArgs([]string{"me"}, emptyInput(t)); err == nil {
		t.Fatal("empty message from empty input: expected an error")
	}
	if _, err := parseSendArgs([]string{"me", "--file", dir}, emptyInput(t)); err == nil {
		t.Fatal("folder as file: expected an error")
	}
	if _, err := parseSendArgs([]string{"me", "--file"}, emptyInput(t)); err == nil {
		t.Fatal("missing file path: expected an error")
	}

	// Message from piped input.
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte("tests passed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	req, err = parseSendArgs([]string{"me"}, f)
	if err != nil || req.Text != "tests passed" {
		t.Fatalf("stdin: got %+v, %v", req, err)
	}
}
