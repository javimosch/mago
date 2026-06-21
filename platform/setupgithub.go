package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// setupgithub.go drives GitHub's App Manifest flow — the only way to create a GitHub App without
// hand-filling the web form. A GitHub App cannot be created via a plain token/API; the manifest
// flow needs exactly ONE browser click. This command serves a local page that POSTs a prefilled
// manifest to GitHub; after the operator clicks "Create GitHub App", GitHub redirects back with a
// temporary code, which we exchange for the App's id, private key, and webhook secret.

type appConversion struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
	HTMLURL       string `json:"html_url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

var manifestForm = template.Must(template.New("f").Parse(`<!doctype html>
<html><body onload="document.forms[0].submit()">
<p>Creating the <b>{{.Name}}</b> GitHub App… if this doesn't redirect, click the button.</p>
<form action="{{.Action}}" method="post">
  <input type="hidden" name="manifest" value="{{.Manifest}}">
  <button type="submit">Create GitHub App</button>
</form>
</body></html>`))

func cmdSetupGithub(args []string) error {
	url, name, org, port := "", "mago", "", "9300"
	out := expand("~/.mago-platform/github-app.env")
	pemPath := expand("~/.mago-platform/github-app.pem")
	for i := 0; i < len(args); i++ {
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--url":
			url = next()
		case "--name":
			name = next()
		case "--org":
			org = next()
		case "--port":
			port = next()
		case "--out":
			out = next()
		case "--pem":
			pemPath = next()
		}
	}
	if url == "" {
		return fmt.Errorf("--url <public platform base, e.g. https://mago.intrane.fr> required")
	}
	base := strings.TrimRight(url, "/")

	stateB := make([]byte, 16)
	rand.Read(stateB)
	state := hex.EncodeToString(stateB)

	manifest, _ := json.Marshal(map[string]any{
		"name":            name,
		"url":             base,
		"hook_attributes": map[string]any{"url": base + "/webhooks/github/", "active": true},
		"redirect_url":    fmt.Sprintf("http://localhost:%s/callback", port),
		"public":          false,
		"default_events":  []string{"issues", "issue_comment", "pull_request"},
		// read-only is enough for v1: the worker still acts via its own gh.
		"default_permissions": map[string]string{"issues": "write", "pull_requests": "write", "contents": "read", "metadata": "read"},
	})
	action := "https://github.com/settings/apps/new?state=" + state
	if org != "" {
		action = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?state=%s", org, state)
	}

	result := make(chan any, 1) // appConversion on success, error on failure
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		manifestForm.Execute(w, map[string]string{"Name": name, "Action": action, "Manifest": string(manifest)})
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "bad state", 400)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", 400)
			return
		}
		conv, err := convertManifest(code)
		if err != nil {
			http.Error(w, "conversion failed: "+err.Error(), 500)
			result <- err
			return
		}
		fmt.Fprintf(w, "GitHub App %q created — return to the terminal; you can close this tab.", conv.Slug)
		result <- *conv
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	go srv.ListenAndServe()
	fmt.Printf("Open this URL in your browser, then click \"Create GitHub App\" (one click):\n\n  http://localhost:%s/\n\n", port)
	fmt.Println("Waiting for GitHub… (Ctrl-C to abort)")

	var conv appConversion
	select {
	case v := <-result:
		if c, ok := v.(appConversion); ok {
			conv = c
		} else {
			return fmt.Errorf("%v", v)
		}
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("timed out waiting for the browser step")
	}
	srv.Shutdown(context.Background())

	os.MkdirAll(filepath.Dir(out), 0o755)
	if err := os.WriteFile(pemPath, []byte(conv.PEM), 0o600); err != nil {
		return err
	}
	env := fmt.Sprintf("GITHUB_APP_ID=%d\nGITHUB_APP_SLUG=%s\nGITHUB_WEBHOOK_SECRET=%s\nGITHUB_APP_PRIVATE_KEY=%s\nGITHUB_APP_CLIENT_ID=%s\nGITHUB_APP_CLIENT_SECRET=%s\n",
		conv.ID, conv.Slug, conv.WebhookSecret, pemPath, conv.ClientID, conv.ClientSecret)
	if err := os.WriteFile(out, []byte(env), 0o600); err != nil {
		return err
	}
	fmt.Printf("\n✓ Created GitHub App %q (id %d), owner %s\n", conv.Slug, conv.ID, conv.Owner.Login)
	fmt.Printf("  private key -> %s\n  credentials -> %s\n", pemPath, out)
	fmt.Printf("  install on your repos: %s/installations/new\n", conv.HTMLURL)
	fmt.Println("\nNext: merge those vars into the platform .env (GITHUB_APP_ID turns on entitlement;")
	fmt.Println("GITHUB_WEBHOOK_SECRET becomes the App's secret), deploy + restart, then `mago link` per install.")
	return nil
}

// convertManifest exchanges the temporary manifest code for the App's credentials (no auth).
func convertManifest(code string) (*appConversion, error) {
	req, _ := http.NewRequest("POST", "https://api.github.com/app-manifests/"+code+"/conversions", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var conv appConversion
	if err := json.Unmarshal(body, &conv); err != nil {
		return nil, err
	}
	return &conv, nil
}
