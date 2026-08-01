package main

import "testing"

func TestParseFeedbackArgs(t *testing.T) {
	msg, ftype := parseFeedbackArgs([]string{"worker", "doctor", "is", "confusing"})
	if msg != "worker doctor is confusing" || ftype != "note" {
		t.Errorf("default type: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"--type", "bug", "relay", "drops", "events"})
	if msg != "relay drops events" || ftype != "bug" {
		t.Errorf("--type parse: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"add", "a", "-t", "feature", "csv", "export"})
	if msg != "add a csv export" || ftype != "feature" {
		t.Errorf("-t mid-args: msg=%q type=%q", msg, ftype)
	}
	if m, _ := parseFeedbackArgs(nil); m != "" {
		t.Errorf("empty args -> empty msg, got %q", m)
	}
}
