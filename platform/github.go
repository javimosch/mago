package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// github.go is the operator-side GitHub integration. It uses the operator's token to provision
// the webhook ingress on customer repos/orgs — the zero-browser-click path to receiving live
// GitHub events (the alternative to a GitHub App, which can't be created via API). The customer
// worker never needs this; it's operator tooling, so it lives in the private platform module.

// githubToken reads the operator token from $GITHUB_TOKEN, else $GITHUB_TOKEN_FILE, else
// ~/.github/token.
func githubToken() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	b, _ := os.ReadFile(expand(env("GITHUB_TOKEN_FILE", "~/.github/token")))
	return strings.TrimSpace(string(b))
}

// ghAPI performs a REST call against api.github.com with the operator token.
func ghAPI(method, path string, body, out any) error {
	tok := githubToken()
	if tok == "" {
		return fmt.Errorf("no GitHub token (set GITHUB_TOKEN, GITHUB_TOKEN_FILE, or ~/.github/token)")
	}
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "https://api.github.com"+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github %s %s -> %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type ghHook struct {
	ID     int64    `json:"id"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

var relayEvents = []string{"issues", "issue_comment", "pull_request"}

// hookBase is "repos/<owner>/<repo>" or "orgs/<org>" — the GitHub webhook collection path.
func hookBase(repo, org string) (string, error) {
	switch {
	case repo != "":
		if !strings.Contains(repo, "/") {
			return "", fmt.Errorf("--repo must be owner/repo")
		}
		return "repos/" + repo, nil
	case org != "":
		return "orgs/" + org, nil
	}
	return "", fmt.Errorf("need --repo owner/repo or --org <org>")
}

// ensureWebhook idempotently points a repo/org webhook at the platform ingress with the shared
// secret. It updates an existing hook with the same URL, or creates one. Returns (hookID, created).
func ensureWebhook(repo, org, ingress, secret string) (int64, bool, error) {
	base, err := hookBase(repo, org)
	if err != nil {
		return 0, false, err
	}
	if secret == "" {
		return 0, false, fmt.Errorf("no webhook secret (set GITHUB_WEBHOOK_SECRET so the ingress can verify signatures)")
	}
	var hooks []ghHook
	if err := ghAPI("GET", "/"+base+"/hooks", nil, &hooks); err != nil {
		return 0, false, err
	}
	cfg := map[string]string{"url": ingress, "content_type": "json", "secret": secret, "insecure_ssl": "0"}
	payload := map[string]any{"name": "web", "active": true, "events": relayEvents, "config": cfg}
	for _, h := range hooks {
		if h.Config.URL == ingress {
			err := ghAPI("PATCH", fmt.Sprintf("/%s/hooks/%d", base, h.ID), payload, nil)
			return h.ID, false, err
		}
	}
	var created ghHook
	if err := ghAPI("POST", "/"+base+"/hooks", payload, &created); err != nil {
		return 0, false, err
	}
	return created.ID, true, nil
}

func listWebhooks(repo, org string) ([]ghHook, error) {
	base, err := hookBase(repo, org)
	if err != nil {
		return nil, err
	}
	var hooks []ghHook
	return hooks, ghAPI("GET", "/"+base+"/hooks", nil, &hooks)
}

func deleteWebhook(repo, org string, id int64) error {
	base, err := hookBase(repo, org)
	if err != nil {
		return err
	}
	return ghAPI("DELETE", fmt.Sprintf("/%s/hooks/%d", base, id), nil, nil)
}

func pingWebhook(repo, org string, id int64) error {
	base, err := hookBase(repo, org)
	if err != nil {
		return err
	}
	return ghAPI("POST", fmt.Sprintf("/%s/hooks/%d/pings", base, id), nil, nil)
}

// cmdWebhook is the operator CLI: `mago-platform webhook add|list|rm|ping`.
func cmdWebhook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mago-platform webhook <add|list|rm|ping> --repo owner/repo|--org <org> [--url https://host] [--account <email>] [--id N]")
	}
	sub := args[0]
	var repo, org, urlFlag, secret, account string
	var id int64
	for i := 1; i < len(args); i++ {
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--repo":
			repo = next()
		case "--org":
			org = next()
		case "--url":
			urlFlag = next()
		case "--secret":
			secret = next()
		case "--account":
			account = next()
		case "--id":
			id = atoi(next())
		}
	}
	if secret == "" {
		secret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}

	switch sub {
	case "add":
		if urlFlag == "" {
			urlFlag = os.Getenv("APP_URL")
		}
		if urlFlag == "" {
			return fmt.Errorf("--url <public-platform-host> required (where GitHub delivers webhooks)")
		}
		ingress := strings.TrimRight(urlFlag, "/")
		if !strings.Contains(ingress, "/webhooks/github") {
			ingress += "/webhooks/github/"
		}
		hid, created, err := ensureWebhook(repo, org, ingress, secret)
		if err != nil {
			return err
		}
		fmt.Printf("%s webhook %d on %s%s -> %s (events: %s)\n",
			map[bool]string{true: "created", false: "updated"}[created],
			hid, repo, org, ingress, strings.Join(relayEvents, ", "))
		if account != "" && repo != "" {
			st, err := openStore(expand(env("DB_PATH", "~/.mago-platform/platform.db")))
			if err != nil {
				return err
			}
			defer st.Close()
			u := st.GetByEmail(strings.ToLower(strings.TrimSpace(account)))
			if u == nil {
				return fmt.Errorf("no account %q to grant — webhook is live, but set entitlement once the user exists", account)
			}
			if err := st.GrantRepo(u.ID, repo); err != nil {
				return err
			}
			fmt.Printf("granted %s to account %s (id %d) — its worker may now receive these events\n", repo, u.Email, u.ID)
		}
		return nil
	case "list":
		hooks, err := listWebhooks(repo, org)
		if err != nil {
			return err
		}
		if len(hooks) == 0 {
			fmt.Println("(no webhooks)")
		}
		for _, h := range hooks {
			fmt.Printf("  #%d active=%v %s [%s]\n", h.ID, h.Active, h.Config.URL, strings.Join(h.Events, ","))
		}
		return nil
	case "rm":
		if id == 0 {
			return fmt.Errorf("--id required")
		}
		if err := deleteWebhook(repo, org, id); err != nil {
			return err
		}
		fmt.Printf("deleted webhook %d\n", id)
		return nil
	case "ping":
		if id == 0 {
			return fmt.Errorf("--id required")
		}
		if err := pingWebhook(repo, org, id); err != nil {
			return err
		}
		fmt.Printf("pinged webhook %d\n", id)
		return nil
	}
	return fmt.Errorf("unknown subcommand %q", sub)
}
