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

func TestCliVersion_MissingDirAndBinary(t *testing.T) {
	// Empty (and whitespace-only) MAGO_CLI_DIR must short-circuit to empty.
	t.Setenv("MAGO_CLI_DIR", "   ")
	t.Setenv("MAGO_CLI_BINARY", "") // keep the legacy fallback out of this assertion
	if got := cliVersion("linux-amd64"); got != "" {
		t.Errorf("cliVersion with whitespace-only MAGO_CLI_DIR = %q, want empty", got)
	}

	dir := t.TempDir()
	t.Setenv("MAGO_CLI_DIR", dir)

	// Missing binary for this platform returns empty.
	if got := cliVersion("linux-arm64"); got != "" {
		t.Errorf("cliVersion for missing binary = %q, want empty", got)
	}
}

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

func TestRelayHub(t *testing.T) {
	h := newRelayHub()

	c1 := &relayConn{
		license: "lic-1",
		worker:  "w1",
		key:     "lic-1\x00w1",
		repos:   map[string]bool{"owner/repo": true},
		ch:      make(chan relayMsg, 1),
	}

	// First registration returns nil and stores the worker.
	if old := h.register(c1); old != nil {
		t.Fatalf("register first conn returned %v, want nil", old)
	}

	// Routing a matching event delivers it.
	if got := h.route("owner/repo", relayMsg{Event: "issues"}); got != 1 {
		t.Fatalf("route returned %d, want 1", got)
	}
	select {
	case msg := <-c1.ch:
		if msg.Event != "issues" {
			t.Errorf("routed msg.Event = %q, want issues", msg.Event)
		}
	default:
		t.Error("routed message was not delivered to the worker channel")
	}

	// Routing to an unknown repo returns 0.
	if got := h.route("unknown/repo", relayMsg{Event: "push"}); got != 0 {
		t.Fatalf("route(unknown) returned %d, want 0", got)
	}

	// Registering a second worker with the same key displaces the first.
	c2 := &relayConn{
		license: "lic-1",
		worker:  "w1",
		key:     "lic-1\x00w1",
		repos:   map[string]bool{"owner/repo": true},
		ch:      make(chan relayMsg, 1),
	}
	if old := h.register(c2); old != c1 {
		t.Fatalf("register replacement returned %v, want c1", old)
	}
	if _, ok := <-c1.ch; ok {
		t.Error("displaced worker channel was not closed")
	}

	// Unregister only removes the current connection for the key.
	h.unregister(c2)
	if got := h.route("owner/repo", relayMsg{Event: "pull_request"}); got != 0 {
		t.Fatalf("route after unregister returned %d, want 0", got)
	}

	// controlMode sends to all workers for a license.
	c3 := &relayConn{
		license: "lic-2",
		worker:  "w2",
		key:     "lic-2\x00w2",
		repos:   map[string]bool{},
		ch:      make(chan relayMsg, 1),
	}
	h.register(c3)
	hit := h.controlMode("lic-2", "", true, relayMsg{Event: "control"})
	if len(hit) != 1 || hit[0] != "w2" {
		t.Fatalf("controlMode(all) hit = %v, want [w2]", hit)
	}
	select {
	case msg := <-c3.ch:
		if msg.Event != "control" {
			t.Errorf("control msg.Event = %q, want control", msg.Event)
		}
	default:
		t.Error("control message was not delivered")
	}

	// controlMode for a different worker returns empty.
	if hit := h.controlMode("lic-2", "w3", false, relayMsg{}); len(hit) != 0 {
		t.Fatalf("controlMode(other) hit = %v, want empty", hit)
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
