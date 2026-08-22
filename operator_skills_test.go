package main

import (
	"strings"
	"testing"
)

func TestOperatorSkillDescription(t *testing.T) {
	cases := []struct {
		name     string
		md       string
		expected string
	}{
		{
			name:     "frontmatter description",
			md:       "---\nname: foo\ndescription: the foo skill\n---\n\nbody\n",
			expected: "the foo skill",
		},
		{
			name:     "description with leading/trailing spaces",
			md:       "---\nname: foo\ndescription:   padded skill   \n---\n",
			expected: "padded skill",
		},
		{
			name:     "no description falls back to heading",
			md:       "---\nname: bar\n---\n\n## Heading here\n\nbody\n",
			expected: "Heading here",
		},
		{
			name:     "no description falls back to first non-meta line",
			md:       "name: baz\n\nfirst real line\n",
			expected: "first real line",
		},
		{
			name:     "no content returns empty",
			md:       "",
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := operatorSkillDescription(tc.md)
			if got != tc.expected {
				t.Fatalf("operatorSkillDescription(%q) = %q, want %q", tc.md, got, tc.expected)
			}
		})
	}
}

func TestCmdSkills_List(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdSkills(nil); err != nil {
			t.Fatalf("cmdSkills: %v", err)
		}
	})

	if !strings.Contains(out, version+" — operator skills") {
		t.Fatalf("expected header with version, got:\n%s", out)
	}
	for _, name := range []string{"cli", "fleet", "operating"} {
		if !strings.Contains(out, name) {
			t.Fatalf("expected output to list skill %q, got:\n%s", name, out)
		}
	}
}

func TestCmdSkills_PrintOne(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdSkills([]string{"cli"}); err != nil {
			t.Fatalf("cmdSkills: %v", err)
		}
	})

	if !strings.Contains(out, "mago CLI reference") {
		t.Fatalf("expected skill content, got:\n%s", out)
	}
}

func TestCmdSkills_Unknown(t *testing.T) {
	if err := cmdSkills([]string{"nope"}); err == nil {
		t.Fatal("expected error for unknown skill")
	} else if !strings.Contains(err.Error(), "no skill \"nope\"") {
		t.Fatalf("expected actionable error, got: %v", err)
	}
}

func TestCmdSkills_SuffixStripped(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdSkills([]string{"cli.md"}); err != nil {
			t.Fatalf("cmdSkills: %v", err)
		}
	})

	if !strings.Contains(out, "mago CLI reference") {
		t.Fatalf("expected .md suffix to be stripped, got:\n%s", out)
	}
}
