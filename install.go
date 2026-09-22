package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// install.go implements `mago install` / `mago uninstall` (cli-update-spec §6): a binary already
// in hand relocates itself to a stable prefix — default ~/.local/bin, no sudo, no rc-file edits.
// This is the fix for workers deployed under a root-owned prefix (e.g. /usr/local/bin): the agent
// user runs `mago install`, which copies the running binary into its own ~/.local/bin — a prefix
// self-update can actually write to.

func cmdInstall(args []string) error {
	prefix, err := resolvePrefix(args)
	if err != nil {
		return err
	}
	exe, err := os.Executable() // resolves to the real file — a symlinked invocation copies bytes
	if err != nil {
		return err
	}
	dest, err := installFile(exe, prefix)
	if err != nil {
		return err
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "installed": dest})
	fmt.Println(string(out))
	if !onPATH(prefix) {
		fmt.Fprintf(os.Stderr, "note: %s is not on $PATH — add it, or invoke %s directly\n", prefix, dest)
	}
	return nil
}

func cmdUninstall(args []string) error {
	prefix, err := resolvePrefix(args)
	if err != nil {
		return err
	}
	dest, removed, err := uninstallFile(prefix)
	if err != nil {
		return err
	}
	out, _ := json.Marshal(map[string]any{"ok": true, "removed": removed, "path": dest})
	fmt.Println(string(out))
	return nil
}

// resolvePrefix parses [--prefix DIR] (also --prefix=DIR), defaulting to ~/.local/bin — the
// spec's no-sudo default, already on $PATH for most dev setups. `~` is expanded and the result
// made absolute so the printed path is unambiguous.
func resolvePrefix(args []string) (string, error) {
	prefix, set := "", false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--prefix":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", &cliErr{80, "--prefix needs a directory value, e.g. `--prefix ~/.local/bin`"}
			}
			prefix, set = strings.TrimSpace(args[i+1]), true
			i++
		case strings.HasPrefix(a, "--prefix="):
			prefix, set = strings.TrimSpace(strings.TrimPrefix(a, "--prefix=")), true
		default:
			return "", &cliErr{80, fmt.Sprintf("unknown argument %q — usage: mago install|uninstall [--prefix DIR]", a)}
		}
	}
	if set && prefix == "" {
		return "", &cliErr{80, "--prefix must not be empty"}
	}
	if prefix == "" {
		prefix = "~/.local/bin"
	}
	if home, err := os.UserHomeDir(); err == nil {
		switch {
		case prefix == "~":
			prefix = home
		case strings.HasPrefix(prefix, "~/"):
			prefix = filepath.Join(home, prefix[2:])
		}
	}
	return filepath.Abs(prefix)
}

// installFile copies src (the running binary) to <prefix>/mago via a temp+rename, so installing
// over an existing — even currently-running — install is safe and idempotent (§6: installing
// twice must succeed). Permission failures come back as the typed §6 error (exit 90).
func installFile(src, prefix string) (string, error) {
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return "", permErr(prefix, err)
	}
	tmp := filepath.Join(prefix, fmt.Sprintf(".mago-install-%d", os.Getpid()))
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		os.Remove(tmp)
		return "", permErr(prefix, err)
	}
	dest := filepath.Join(prefix, "mago")
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", permErr(prefix, err)
	}
	return dest, nil
}

// uninstallFile removes <prefix>/mago and nothing else (§6: no config, no data, no daemon
// registration). A missing file is a successful no-op, not an error.
func uninstallFile(prefix string) (dest string, removed bool, err error) {
	dest = filepath.Join(prefix, "mago")
	if err := os.Remove(dest); err != nil {
		if os.IsNotExist(err) {
			return dest, false, nil
		}
		if isPermErr(err) {
			return "", false, &cliErr{90, fmt.Sprintf("permission denied removing %s as %s", dest, currentUser())}
		}
		return "", false, err
	}
	return dest, true, nil
}

// permErr wraps filesystem permission failures in the typed §6 error (exit 90) naming the target
// and the running user — the operator's cue that escalating or retrying is wrong; pick a writable
// prefix instead.
func permErr(dir string, err error) error {
	if isPermErr(err) {
		return &cliErr{90, fmt.Sprintf("permission denied writing %s as %s — choose a writable --prefix (default ~/.local/bin needs no sudo)", dir, currentUser())}
	}
	return err
}

// onPATH reports whether dir is an entry in $PATH (for the post-install note).
func onPATH(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return true
		}
	}
	return false
}
