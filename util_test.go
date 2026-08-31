package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{"multibyte runes", "café", 3, "caf…"},
		{"emoji", "👋 hello", 3, "👋 h…"},
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

func TestDirListing(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty directory", func(t *testing.T) {
		if got := dirListing(dir); got != "(empty)" {
			t.Errorf("got %q, want %q", got, "(empty)")
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		if got := dirListing(filepath.Join(dir, "nope")); got != "(empty)" {
			t.Errorf("got %q, want %q", got, "(empty)")
		}
	})

	t.Run("lists files and directories", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		got := dirListing(dir)
		want := "- a.txt\n- sub/\n"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
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

func TestNowStamp(t *testing.T) {
	got := nowStamp()
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("nowStamp() = %q, want UTC 'Z' suffix", got)
	}
	if strings.ContainsAny(got, ":/") {
		t.Errorf("nowStamp() = %q, should not contain filesystem-unsafe ':' or '/'", got)
	}
	// Sanity-check that it parses back as a UTC timestamp with the dash separator format.
	if _, err := time.Parse("2006-01-02T15-04-05Z", got); err != nil {
		t.Errorf("nowStamp() = %q, does not match expected format: %v", got, err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "a.txt")
	dst := filepath.Join(dir, "dst", "a.txt")

	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	copyFile(src, dst)

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("copyFile did not create destination: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("copied content = %q, want %q", got, "hello")
	}

	// Missing source is a silent no-op.
	copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "out"))
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Errorf("copyFile from missing source should not create a destination")
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")

	if err := ensureDir(nested); err != nil {
		t.Fatalf("ensureDir(%q) error: %v", nested, err)
	}

	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("ensureDir did not create %q: %v", nested, err)
	}
	if !info.IsDir() {
		t.Errorf("ensureDir(%q) created a non-directory", nested)
	}
}

func TestCopyTree(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	copyTree(src, dst)

	for _, p := range []string{
		filepath.Join(dst, "a.txt"),
		filepath.Join(dst, "sub", "b.txt"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("copyTree did not create %q: %v", p, err)
		}
	}

	got, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if string(got) != "nested" {
		t.Errorf("copied nested content = %q, want %q", got, "nested")
	}

	// Missing source is a silent no-op.
	copyTree(filepath.Join(dir, "missing"), filepath.Join(dir, "out"))
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Errorf("copyTree from missing source should not create a destination")
	}
}
