package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPlatOf(t *testing.T) {
	cases := []struct {
		os, arch, want string
	}{
		{"linux", "amd64", "linux-amd64"},
		{"darwin", "arm64", "darwin-arm64"},
		{"", "", "linux-amd64"},
		{"", "arm64", "linux-arm64"},
		{"windows", "", "windows-amd64"},
	}
	for _, c := range cases {
		got := platOf(c.os, c.arch)
		if got != c.want {
			t.Errorf("platOf(%q, %q) = %q, want %q", c.os, c.arch, got, c.want)
		}
	}
}

func TestKeys(t *testing.T) {
	got := keys(map[string]bool{"b": true, "a": true, "c": true})
	want := []string{"a", "b", "c"}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("keys() returned %d items, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("keys()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidGithubSig(t *testing.T) {
	secret := "webhook-secret"
	body := []byte("payload")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validGithubSig(secret, good, body) {
		t.Error("validGithubSig rejected a valid signature")
	}
	if validGithubSig("wrong-secret", good, body) {
		t.Error("validGithubSig accepted a signature with the wrong secret")
	}
	if validGithubSig(secret, good+"x", body) {
		t.Error("validGithubSig accepted a tampered signature")
	}
	if validGithubSig(secret, "sha256=deadbeef", body) {
		t.Error("validGithubSig accepted an invalid hex signature")
	}
	if validGithubSig(secret, strings.TrimPrefix(good, "sha256="), body) {
		t.Error("validGithubSig accepted a signature missing the sha256= prefix")
	}
	if validGithubSig("", good, body) {
		t.Error("validGithubSig accepted a signature with an empty secret")
	}
}

func TestRepoHash(t *testing.T) {
	cases := []string{"owner/repo", "a/b", "javimosch/mago"}
	for _, repo := range cases {
		got := repoHash(repo)
		h := fnv.New32a()
		h.Write([]byte(repo))
		want := h.Sum32()
		if got != want {
			t.Errorf("repoHash(%q) = %d, want %d", repo, got, want)
		}
	}

	// Different repos should almost certainly produce different hashes.
	a, b := repoHash("foo/bar"), repoHash("bar/foo")
	if a == b {
		t.Errorf("repoHash collisions: foo/bar and bar/foo both hash to %d", a)
	}
}

func TestCliVersionTrimsMAGOCLIDir(t *testing.T) {
	dir := t.TempDir()
	plat := "linux-amd64"
	bin := filepath.Join(dir, "mago-"+plat)
	payload := []byte("fake-cli-binary")
	if err := os.WriteFile(bin, payload, 0o644); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	// Reset the per-platform version cache so a previous run cannot mask the bug.
	cliVerMu.Lock()
	cliVerCache = map[string]cliVerEntry{}
	cliVerMu.Unlock()

	// Whitespace around MAGO_CLI_DIR must be ignored; without trimming,
	// filepath.Join would try a path that does not exist.
	t.Setenv("MAGO_CLI_DIR", " "+dir+" ")
	got := cliVersion(plat)
	if got == "" {
		t.Fatal("cliVersion returned empty for a whitespace-padded MAGO_CLI_DIR")
	}

	h := sha256.New()
	h.Write(payload)
	want := hex.EncodeToString(h.Sum(nil))[:12]
	if got != want {
		t.Errorf("cliVersion(%q) = %q, want %q", plat, got, want)
	}
}
