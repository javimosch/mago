package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseFrontmatterBasic verifies that a well-formed header is split into a
// key/value map and the remaining body, with surrounding quotes stripped from
// values and leading blank lines trimmed from the body.
func TestParseFrontmatterBasic(t *testing.T) {
	content := "---\n" +
		"name: alice\n" +
		"title: \"Chief Marketing Officer\"\n" +
		"reviews: true\n" +
		"---\n" +
		"\n" +
		"You are the CMO.\n"

	fm, body := parseFrontmatter(content)

	want := map[string]string{
		"name":    "alice",
		"title":   "Chief Marketing Officer", // quotes stripped
		"reviews": "true",
	}
	if !reflect.DeepEqual(fm, want) {
		t.Errorf("frontmatter map = %#v, want %#v", fm, want)
	}
	if body != "You are the CMO.\n" {
		t.Errorf("body = %q, want %q", body, "You are the CMO.\n")
	}
}

// TestParseFrontmatterValueWithColon verifies that only the first colon
// separates key from value, so values may themselves contain colons.
func TestParseFrontmatterValueWithColon(t *testing.T) {
	content := "---\n" +
		"model: provider:gpt-4o:latest\n" +
		"---\nbody\n"

	fm, _ := parseFrontmatter(content)
	if got := fm["model"]; got != "provider:gpt-4o:latest" {
		t.Errorf("model = %q, want %q", got, "provider:gpt-4o:latest")
	}
}

// TestParseFrontmatterSkipsLinesWithoutColon verifies that header lines lacking
// a colon are ignored rather than producing bogus entries.
func TestParseFrontmatterSkipsLinesWithoutColon(t *testing.T) {
	content := "---\n" +
		"name: bob\n" +
		"this line has no colon\n" +
		"title: Engineer\n" +
		"---\nbody\n"

	fm, _ := parseFrontmatter(content)
	want := map[string]string{"name": "bob", "title": "Engineer"}
	if !reflect.DeepEqual(fm, want) {
		t.Errorf("frontmatter map = %#v, want %#v", fm, want)
	}
}

// TestParseFrontmatterMalformed covers inputs that should yield an empty map and
// the original content returned unchanged as the body: no leading delimiter and
// a missing closing delimiter.
func TestParseFrontmatterMalformed(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "no leading delimiter",
			content: "name: alice\ntitle: CMO\nbody text\n",
		},
		{
			name:    "missing closing delimiter",
			content: "---\nname: alice\ntitle: CMO\nstill no terminator\n",
		},
		{
			name:    "empty input",
			content: "",
		},
		{
			name:    "plain body only",
			content: "just some markdown\nwith no header at all\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, body := parseFrontmatter(tc.content)
			if len(fm) != 0 {
				t.Errorf("expected empty frontmatter, got %#v", fm)
			}
			if body != tc.content {
				t.Errorf("body = %q, want unchanged content %q", body, tc.content)
			}
		})
	}
}

// TestParseFrontmatterEmptyBlock verifies that a header with no keys yields an
// empty map and exposes the body that follows the closing delimiter.
func TestParseFrontmatterEmptyBlock(t *testing.T) {
	content := "---\n---\nhello\n"
	fm, body := parseFrontmatter(content)
	if len(fm) != 0 {
		t.Errorf("expected empty frontmatter, got %#v", fm)
	}
	if body != "hello\n" {
		t.Errorf("body = %q, want %q", body, "hello\n")
	}
}

// TestRenderFrontmatterKeyOrdering verifies that keys are emitted strictly in
// the supplied order, that keys absent from the map are skipped, and that keys
// present in the map but absent from the order are omitted entirely.
func TestRenderFrontmatterKeyOrdering(t *testing.T) {
	fm := map[string]string{
		"title":    "CMO",
		"name":     "alice",
		"provider": "openai",
		"ignored":  "should-not-appear", // not in order → omitted
	}
	order := []string{"name", "title", "missing", "provider"}

	got := renderFrontmatter(fm, order, "body\n")
	want := "---\n" +
		"name: alice\n" +
		"title: CMO\n" +
		"provider: openai\n" +
		"---\n" +
		"body\n"
	if got != want {
		t.Errorf("render mismatch:\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "ignored") {
		t.Error("rendered output included a key absent from the order slice")
	}
}

// TestRenderFrontmatterBodyNewline verifies the body handling: an empty body
// emits only the header, and a body without a trailing newline gets one added.
func TestRenderFrontmatterBodyNewline(t *testing.T) {
	fm := map[string]string{"name": "alice"}
	order := []string{"name"}

	if got := renderFrontmatter(fm, order, ""); got != "---\nname: alice\n---\n" {
		t.Errorf("empty body render = %q, want %q", got, "---\nname: alice\n---\n")
	}

	// Body lacking a trailing newline should get exactly one appended.
	got := renderFrontmatter(fm, order, "no newline")
	if want := "---\nname: alice\n---\nno newline\n"; got != want {
		t.Errorf("render = %q, want %q", got, want)
	}

	// Body already ending in a newline should not gain a second one.
	got = renderFrontmatter(fm, order, "has newline\n")
	if want := "---\nname: alice\n---\nhas newline\n"; got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

// TestFrontmatterRoundTrip verifies that rendering a map and parsing it back
// reproduces the original key/value pairs and body, which is the invariant the
// agent and task persistence layers rely on.
func TestFrontmatterRoundTrip(t *testing.T) {
	original := map[string]string{
		"name":     "alice",
		"title":    "Chief Marketing Officer",
		"provider": "openai",
		"model":    "gpt-4o",
		"reviews":  "true",
	}
	order := []string{"name", "title", "provider", "model", "reviews"}
	body := "You are the CMO.\nWrite clear copy.\n"

	rendered := renderFrontmatter(original, order, body)
	gotFM, gotBody := parseFrontmatter(rendered)

	if !reflect.DeepEqual(gotFM, original) {
		t.Errorf("round-trip frontmatter = %#v, want %#v", gotFM, original)
	}
	if gotBody != body {
		t.Errorf("round-trip body = %q, want %q", gotBody, body)
	}
}

// TestFrontmatterRoundTripPreservesOrder verifies that the rendered header lists
// keys in the exact order supplied, so a parse → render cycle with a stable
// order slice is byte-for-byte stable (no map-iteration nondeterminism leaks in).
func TestFrontmatterRoundTripPreservesOrder(t *testing.T) {
	content := "---\n" +
		"name: alice\n" +
		"title: CMO\n" +
		"provider: openai\n" +
		"---\n" +
		"persona body\n"
	order := []string{"name", "title", "provider"}

	fm, body := parseFrontmatter(content)
	rerendered := renderFrontmatter(fm, order, body)

	if rerendered != content {
		t.Errorf("re-render not stable:\n got: %q\nwant: %q", rerendered, content)
	}
}
