package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"lowercases", "Hello WORLD", []string{"hello", "world"}},
		{"drops short words", "a to the cat fish", []string{"fish"}}, // only >=4 runes kept ("the"=3, "cat"=3)
		{"splits on non-alphanumeric", "route-test_llm.fallback", []string{"route", "test", "fallback"}},
		{"keeps digits", "go1234 abcd", []string{"go1234", "abcd"}},
		{"dedups repeats", "test test test", []string{"test"}},
		{"strips underscores as separators", "snake_case_word", []string{"snake", "case", "word"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenize(tc.in)
			var keys []string
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(keys, want) {
				t.Errorf("tokenize(%q) = %v, want %v", tc.in, keys, want)
			}
		})
	}
}

func TestTokenizeMinLengthBoundary(t *testing.T) {
	// "abc" (3) excluded, "abcd" (4) included — confirms the >=4 boundary.
	m := tokenize("abc abcd")
	if m["abc"] {
		t.Error("3-rune token should be excluded")
	}
	if !m["abcd"] {
		t.Error("4-rune token should be included")
	}
}

func TestParseStringArray(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"plain array", `["a","b"]`, []string{"a", "b"}},
		{"empty array", `[]`, []string{}},
		{"fenced", "```json\n[\"x\",\"y\"]\n```", []string{"x", "y"}},
		{"surrounding prose", `Here you go: ["one","two"] thanks`, []string{"one", "two"}},
		{"no brackets", `just text`, nil},
		{"malformed json", `[not, valid, json]`, nil},
		{"reversed brackets", `]nope[`, nil},
		{"non-string elements", `[1,2,3]`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStringArray(tc.in)
			// Treat empty-but-non-nil and nil as distinct where the test expects it.
			if tc.want == nil {
				if got != nil {
					t.Errorf("parseStringArray(%q) = %v, want nil", tc.in, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseStringArray(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestKeywordSelectRanking(t *testing.T) {
	entries := []skillEntry{
		{name: "routing-tests", hook: "router fallback retries with backoff"},
		{name: "frontmatter-helpers", hook: "parse and render frontmatter round-trip"},
		{name: "unrelated-skill", hook: "nothing matches here xxxx"},
	}
	// Task tokens overlap routing-tests on "router"/"fallback"/"backoff", and
	// frontmatter-helpers on "frontmatter".
	task := &Task{Title: "router fallback backoff", Body: "frontmatter"}

	got := keywordSelect(task, entries, 4)
	if len(got) != 2 {
		t.Fatalf("expected 2 matching skills, got %v", got)
	}
	// routing-tests has 3 overlapping tokens vs frontmatter-helpers' 1 — must rank first.
	if got[0] != "routing-tests" {
		t.Errorf("expected routing-tests ranked first, got %v", got)
	}
}

func TestKeywordSelectLimit(t *testing.T) {
	entries := []skillEntry{
		{name: "skill-alpha", hook: "shared keyword token"},
		{name: "skill-beta", hook: "shared keyword token"},
		{name: "skill-gamma", hook: "shared keyword token"},
	}
	task := &Task{Title: "shared keyword token", Body: ""}
	got := keywordSelect(task, entries, 2)
	if len(got) != 2 {
		t.Errorf("limit k=2 not honored, got %d entries: %v", len(got), got)
	}
}

func TestKeywordSelectNoMatch(t *testing.T) {
	entries := []skillEntry{
		{name: "skill-one", hook: "completely different words"},
		{name: "skill-two", hook: "yet more unrelated terms"},
	}
	task := &Task{Title: "zzzz qqqq", Body: ""}
	got := keywordSelect(task, entries, 4)
	if got != nil {
		t.Errorf("expected nil for no overlap, got %v", got)
	}
}

func TestKeywordSelectShortTokensIgnored(t *testing.T) {
	// Overlap only on a <4-rune token ("api") which tokenize drops -> no match.
	entries := []skillEntry{{name: "skill-x", hook: "api use"}}
	task := &Task{Title: "api", Body: ""}
	if got := keywordSelect(task, entries, 4); got != nil {
		t.Errorf("sub-4-rune overlap should not match, got %v", got)
	}
}

func TestSelectSkillsTextEmpty(t *testing.T) {
	c := newTestCompany(t)
	got := c.selectSkillsText(nil, &Task{Title: "task", Body: "body"})
	if !strings.Contains(got, "(no skills yet)") {
		t.Errorf("empty index should render no-skills message: %q", got)
	}
}

func TestSelectSkillsTextInjectsAll(t *testing.T) {
	c := newTestCompany(t)

	// The em dash (—) is the delimiter readIndexEntries expects.
	index := "- skill-a \u2014 hook-a\n- skill-b \u2014 hook-b\n"
	if err := os.WriteFile(c.skillsIndex(), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	for _, name := range []string{"skill-a", "skill-b"} {
		dir := filepath.Join(c.skillsDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body of "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write %s skill: %v", name, err)
		}
	}

	got := c.selectSkillsText(nil, &Task{Title: "task", Body: "body"})
	for _, want := range []string{"skill-a", "skill-b", "hook-a", "hook-b", "--- skill: skill-a ---", "body of skill-a", "--- skill: skill-b ---", "body of skill-b"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestLLMSelectSkills verifies the LLM skill selector using a fake tau binary.
// It checks that the returned JSON array is filtered to known skills and capped at k.
func TestLLMSelectSkills(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	// tauComplete scans the last JSON line with a "content" field; the prompt
	// itself is irrelevant for this unit test, so the fake just echoes the array.
	body := "#!/bin/sh\necho '{\"content\":\"[\\\"skill-a\\\",\\\"skill-b\\\",\\\"unknown\\\"]\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	a := &Agent{Provider: "deepseek", Model: "deepseek-chat"}
	entries := []skillEntry{
		{name: "skill-a", hook: "hook a"},
		{name: "skill-b", hook: "hook b"},
	}

	cases := []struct {
		name string
		k    int
		want []string
	}{
		{
			name: "filters unknown and caps at k",
			k:    1,
			want: []string{"skill-a"},
		},
		{
			name: "returns multiple known up to k",
			k:    2,
			want: []string{"skill-a", "skill-b"},
		},
		{
			name: "caps at k even when more returned",
			k:    1,
			want: []string{"skill-a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.llmSelectSkills(a, &Task{Title: "task", Body: "body"}, entries, tc.k)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("llmSelectSkills(...) = %v, want %v", got, tc.want)
			}
		})
	}
}
