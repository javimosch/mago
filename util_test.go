package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase passthrough", "abc-123_x", "abc-123_x"},
		{"uppercase folded", "ABC", "abc"},
		{"mixed case", "MyTask", "mytask"},
		{"spaces to dash", "hello world", "hello-world"},
		{"punctuation to dash", "a.b/c:d", "a-b-c-d"},
		{"keeps dash and underscore", "a-b_c", "a-b_c"},
		{"empty", "", ""},
		{"non-ascii to dash", "café", "caf-"},
		{"leading and trailing specials", "*x*", "-x-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitize(c.in); got != c.want {
				t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"equal to limit", "hello", 5, "hello"},
		{"longer than limit", "hello world", 5, "hello…"},
		{"trims before measuring", "  hello  ", 5, "hello"},
		{"trims then truncates", "  hello world  ", 5, "hello…"},
		{"zero limit", "abc", 0, "…"},
		{"empty input", "", 5, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncate(c.in, c.n); got != c.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no newlines", "hello", "hello"},
		{"unix newline", "a\nb", "a b"},
		{"carriage return", "a\rb", "a b"},
		{"crlf", "a\r\nb", "a  b"},
		{"trims surrounding whitespace", "  hello\n", "hello"},
		{"multiple lines", "line1\nline2\nline3", "line1 line2 line3"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := oneLine(c.in); got != c.want {
				t.Errorf("oneLine(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestStripFences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text untouched", "hello", "hello"},
		{"trims whitespace", "  hello  ", "hello"},
		{"json fence", "```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"bare fence", "```\nfoo\n```", "foo"},
		{"fence with surrounding whitespace", "  ```json\n{}\n```  ", "{}"},
		{"multiline body", "```\nline1\nline2\n```", "line1\nline2"},
		{"no closing fence", "```json\n{}", "{}"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripFences(c.in); got != c.want {
				t.Errorf("stripFences(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestReadFileOr(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file returns default", func(t *testing.T) {
		got := readFileOr(filepath.Join(dir, "nope.txt"), "def")
		if got != "def" {
			t.Errorf("got %q, want %q", got, "def")
		}
	})

	t.Run("reads and trims content", func(t *testing.T) {
		p := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(p, []byte("  hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readFileOr(p, "def"); got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("blank content returns default", func(t *testing.T) {
		p := filepath.Join(dir, "blank.txt")
		if err := os.WriteFile(p, []byte("   \n\t"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readFileOr(p, "def"); got != "def" {
			t.Errorf("got %q, want %q", got, "def")
		}
	})
}

func TestOrDefault(t *testing.T) {
	cases := []struct {
		name string
		v    string
		def  string
		want string
	}{
		{"non-empty value kept", "value", "def", "value"},
		{"empty falls back", "", "def", "def"},
		{"whitespace-only falls back", "   \t", "def", "def"},
		{"value with surrounding space is preserved", "  x  ", "def", "  x  "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := orDefault(c.v, c.def); got != c.want {
				t.Errorf("orDefault(%q, %q) = %q, want %q", c.v, c.def, got, c.want)
			}
		})
	}
}
