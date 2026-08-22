package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reflJSON is a minimal valid reflection for parse tests.
const reflJSON = `{"summary":"did work","state_delta":"thing changed","task_status":"done","lessons":[],"next":"next step","cadence_signal":"idle"}`

func TestStripDSML_NoMarkup(t *testing.T) {
	input := reflJSON
	got := stripDSML(input)
	if got != input {
		t.Errorf("stripDSML modified clean input: got %q", got)
	}
}

func TestStripDSML_PureMarkupNoBrace(t *testing.T) {
	input := "<｜｜DSML｜｜tool_calls> name=bash command=ls (no json here)"
	got := stripDSML(input)
	// No '{' — should return original (parseReflection will catch it)
	if got != input {
		t.Errorf("stripDSML(%q) = %q, want original (no brace)", input, got)
	}
}

func TestStripDSML_MarkupThenJSON(t *testing.T) {
	input := "<｜｜DSML｜｜tool_calls> name=bash\n" + reflJSON
	got := stripDSML(input)
	if got != reflJSON {
		t.Errorf("stripDSML did not strip markup prefix:\ngot:  %q\nwant: %q", got, reflJSON)
	}
}

func TestParseReflection_Clean(t *testing.T) {
	r, err := parseReflection("```json\n" + reflJSON + "\n```")
	if err != nil {
		t.Fatalf("parseReflection clean: unexpected error: %v", err)
	}
	if r.Summary != "did work" {
		t.Errorf("got summary %q, want %q", r.Summary, "did work")
	}
}

func TestParseReflection_DSMLWrappingJSON(t *testing.T) {
	input := "<｜｜DSML｜｜tool_calls> name=bash\n" + reflJSON
	r, err := parseReflection(input)
	if err != nil {
		t.Fatalf("parseReflection with DSML wrapper: unexpected error: %v", err)
	}
	if r.Summary != "did work" {
		t.Errorf("got summary %q, want %q", r.Summary, "did work")
	}
}

func TestParseReflection_NoBrace(t *testing.T) {
	input := "<｜｜DSML｜｜tool_calls> name=bash command=ls (no reflection json here)"
	_, err := parseReflection(input)
	if err == nil {
		t.Fatal("parseReflection: expected error for output with no JSON object, got nil")
	}
	if !strings.Contains(err.Error(), "no JSON object") {
		t.Errorf("error should mention 'no JSON object', got: %v", err)
	}
}

func TestParseReflection_EmptyContent(t *testing.T) {
	_, err := parseReflection("")
	if err == nil {
		t.Fatal("parseReflection: expected error for empty content, got nil")
	}
}

func TestJsonCandidates_NoContent(t *testing.T) {
	// jsonCandidates on a string with no braces should return slice entries that all fail to parse.
	cands := jsonCandidates("no json here at all")
	for _, c := range cands {
		if strings.Contains(c, "{") {
			t.Errorf("jsonCandidates returned a candidate with '{' from brace-free input: %q", c)
		}
	}
}

func TestExtractFinalContent_DoneLine(t *testing.T) {
	lines := []string{
		`{"chunk":"hello"}`,
		`{"done":true,"content":"final-content"}`,
	}
	got, err := extractFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "final-content" {
		t.Errorf("got %q, want %q", got, "final-content")
	}
}

func TestExtractFinalContent_FallbackChunks(t *testing.T) {
	lines := []string{
		`{"chunk":"hel"}`,
		`{"chunk":"lo"}`,
	}
	got, err := extractFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractFinalContent_Empty(t *testing.T) {
	_, err := extractFinalContent(nil)
	if err == nil {
		t.Fatal("expected error for empty lines, got nil")
	}
}

func TestEmitProgress(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = old
		w.Close()
	})

	// Non-JSON and JSON without a "chunk" key produce no output.
	emitProgress("not json")
	emitProgress(`{"foo":"bar"}`)

	// A "chunk" value is written straight to stderr.
	emitProgress(`{"chunk":"hello"}`)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "hello" {
		t.Errorf("emitProgress wrote %q, want %q", got, "hello")
	}
}

func TestRunTau(t *testing.T) {
	dir := t.TempDir()
	tauBin := filepath.Join(dir, "tau")
	body := "#!/bin/sh\necho '{\"content\": \"reflection\", \"done\": true}'\n"
	if err := os.WriteFile(tauBin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	workspace := t.TempDir()
	got, err := runTau(workspace, &Agent{Provider: "opencode", Model: "qwen2.5"}, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runTau: %v", err)
	}
	if got != "reflection" {
		t.Errorf("runTau = %q, want reflection", got)
	}
}
