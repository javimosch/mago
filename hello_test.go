package main

import "testing"

func TestHello(t *testing.T) {
	if got := Hello(); got != "hi" {
		t.Fatalf("Hello() = %q, want %q", got, "hi")
	}
}
