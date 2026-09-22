package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestResolvePrefix verifies §6 prefix parsing: --prefix (both forms), ~ expansion,
// the ~/.local/bin default, and typed rejection of malformed input.
func TestResolvePrefix(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{nil, filepath.Join(home, ".local", "bin"), false},
		{[]string{"--prefix", "/opt/tools"}, "/opt/tools", false},
		{[]string{"--prefix=/opt/tools"}, "/opt/tools", false},
		{[]string{"--prefix", "~/bin"}, filepath.Join(home, "bin"), false},
		{[]string{"--prefix"}, "", true},  // missing value
		{[]string{"--prefix="}, "", true}, // empty value
		{[]string{"bogus"}, "", true},     // unknown argument
	}
	for _, c := range cases {
		got, err := resolvePrefix(c.args)
		if c.wantErr {
			var ce *cliErr
			if !errors.As(err, &ce) || ce.code != 80 {
				t.Errorf("resolvePrefix(%v) = %v, want cliErr code 80", c.args, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolvePrefix(%v) error: %v", c.args, err)
		} else if got != c.want {
			t.Errorf("resolvePrefix(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}

// TestInstallFile verifies the §6 install contract: the binary is copied (not moved)
// to <prefix>/mago, marked executable, and re-installing is a success, not an error.
func TestInstallFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "mago")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("binary-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(dir, "nested", "bin") // does not exist yet — install creates it

	dest, err := installFile(src, prefix)
	if err != nil {
		t.Fatalf("installFile: %v", err)
	}
	if dest != filepath.Join(prefix, "mago") {
		t.Errorf("dest = %q, want %q", dest, filepath.Join(prefix, "mago"))
	}
	if b, _ := os.ReadFile(dest); string(b) != "binary-bytes" {
		t.Errorf("installed content = %q", b)
	}
	if fi, _ := os.Stat(dest); fi.Mode()&0o111 == 0 {
		t.Error("installed binary is not executable")
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("install must COPY — the source binary must still exist")
	}

	// Idempotent: installing again over the existing file succeeds.
	if _, err := installFile(src, prefix); err != nil {
		t.Errorf("second installFile = %v, want nil (idempotent)", err)
	}
}

// TestInstallFile_PermDenied verifies the §6 permission contract: an unwritable
// prefix fails with the typed error (exit 90), never an escalation attempt.
func TestInstallFile_PermDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission checks don't apply")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "mago")
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o755) // let TempDir cleanup proceed

	var ce *cliErr
	if _, err := installFile(src, filepath.Join(locked, "bin")); !errors.As(err, &ce) || ce.code != 90 {
		t.Errorf("installFile into unwritable prefix = %v, want cliErr code 90", err)
	}
}

// TestUninstallFile verifies §6 uninstall: removing the binary succeeds, a missing
// binary is a no-op success (removed=false), and nothing else is touched.
func TestUninstallFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "mago")

	// Absent -> no-op success.
	gotDest, removed, err := uninstallFile(dir)
	if err != nil || removed {
		t.Errorf("uninstallFile(absent) = (%v, %v), want (nil, false)", err, removed)
	}
	if gotDest != dest {
		t.Errorf("dest = %q, want %q", gotDest, dest)
	}

	// Present -> removed; neighboring files survive.
	if err := os.WriteFile(dest, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "mago.bak")
	if err := os.WriteFile(other, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, removed, err = uninstallFile(dir)
	if err != nil || !removed {
		t.Errorf("uninstallFile(present) = (%v, %v), want (nil, true)", err, removed)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("binary should be gone after uninstall")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("uninstall must not touch files other than <prefix>/mago")
	}
}

// TestOnPATH verifies the PATH-membership check used for the post-install note.
func TestOnPATH(t *testing.T) {
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/home/x/.local/bin")
	if !onPATH("/home/x/.local/bin") {
		t.Error("onPATH should find a listed dir")
	}
	if onPATH("/usr/local/bin") {
		t.Error("onPATH should reject an unlisted dir")
	}
}
