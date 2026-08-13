package main

import "testing"

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
