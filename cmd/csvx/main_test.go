package main

import (
	"path/filepath"
	"testing"
)

func TestParsePackageArgumentsAllowsFlagsAfterInput(t *testing.T) {
	input, output, help, err := parsePackageArguments([]string{"source", "--output", "result.csvx"})
	if err != nil || help || input != "source" || output != "result.csvx" {
		t.Fatalf("unexpected result: input=%q output=%q help=%v err=%v", input, output, help, err)
	}
}

func TestParseExtractArgumentsAllowsShortOutputFlag(t *testing.T) {
	input, output, help, err := parseExtractArguments([]string{"book.csvx", "-o", "work"})
	if err != nil || help || input != "book.csvx" || output != "work" {
		t.Fatalf("unexpected result: input=%q output=%q help=%v err=%v", input, output, help, err)
	}
}

func TestParsePackageArgumentsRequiresOutput(t *testing.T) {
	_, _, _, err := parsePackageArguments([]string{"source"})
	if err == nil {
		t.Fatal("expected missing output error")
	}
}

func TestOpenInputRecognizesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	if _, err := openInput(path); err == nil {
		t.Fatal("expected missing input error")
	}
}
