package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func copyFile(src, dst string) {
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	ensureDir(filepath.Dir(dst))
	os.WriteFile(dst, b, 0o644)
}

func copyTree(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	ensureDir(dst)
	for _, e := range entries {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyTree(s, d)
		} else {
			copyFile(s, d)
		}
	}
}

// sanitize lowercases and replaces non [a-z0-9-_] runes with '-' (for ids/paths).
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		default:
			return '-'
		}
	}, s)
}

// nowStamp is a filesystem-safe UTC timestamp.
func nowStamp() string { return time.Now().UTC().Format("2006-01-02T15-04-05Z") }

func readFileOr(p, def string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return def
	}
	if s := strings.TrimSpace(string(b)); s != "" {
		return s
	}
	return def
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func ensureDir(p string) error { return os.MkdirAll(p, 0o755) }

func dirListing(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return "(empty)"
	}
	var b strings.Builder
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString("- " + name + "\n")
	}
	return b.String()
}

// stripFences removes a leading ```...``` code fence if the model wrapped its JSON.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}
