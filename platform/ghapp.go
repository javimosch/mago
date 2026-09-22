package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ghapp.go creates GitHub resources AS the mago App — no operator PAT on the box. It builds an RS256
// App JWT from the App private key already configured for the webhook relay, exchanges it for a
// least-privilege installation token (scoped to the one repo, issues:write), and calls the REST API.
// Powers feedback-issue routing (the App manifest already grants issues:write).

func appPrivateKey() (*rsa.PrivateKey, error) {
	v := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if v == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY unset")
	}
	data := []byte(v)
	if !strings.Contains(v, "BEGIN") { // a file path rather than inline PEM
		b, err := os.ReadFile(expand(v))
		if err != nil {
			return nil, fmt.Errorf("read app key %s: %w", v, err)
		}
		data = b
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY: no PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse app key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("app key is not RSA")
	}
	return rk, nil
}

// appJWT builds a short-lived (≤10 min) RS256 JWT identifying the App, per GitHub's spec.
func appJWT() (string, error) {
	key, err := appPrivateKey()
	if err != nil {
		return "", err
	}
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	if appID == "" {
		return "", fmt.Errorf("GITHUB_APP_ID unset")
	}
	now := time.Now().Unix()
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." +
		enc([]byte(fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, now-60, now+540, appID)))
	h := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc(sig), nil
}

// ghDo calls the REST API with a given bearer token (an App JWT or an installation token).
func ghDo(token, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "https://api.github.com"+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

// appInstallationToken mints a least-privilege installation token for repo (owner/name): scoped to
// that single repo, issues:write only.
func appInstallationToken(repo string) (string, error) {
	jwt, err := appJWT()
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := ghDo(jwt, "GET", "/repos/"+repo+"/installation", nil, &inst); err != nil {
		return "", err
	}
	if inst.ID == 0 {
		return "", fmt.Errorf("mago App not installed on %s", repo)
	}
	name := repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		name = repo[i+1:]
	}
	var tok struct {
		Token string `json:"token"`
	}
	err = ghDo(jwt, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID),
		map[string]any{"repositories": []string{name}, "permissions": map[string]string{"issues": "write"}}, &tok)
	if err != nil {
		return "", err
	}
	if tok.Token == "" {
		return "", fmt.Errorf("empty installation token for %s", repo)
	}
	return tok.Token, nil
}

// listInstallationRepos fetches the CURRENT, COMPLETE list of repos an installation can access,
// straight from GitHub, rather than relying on the incrementally-built repos_json cache. Needed
// because an "All repositories" installation does not include a repository list in its
// `installation` webhook payload (only "Only select repositories" installs do) -- so repos_json
// only ever grows via installation_repositories add/remove deltas and never reflects the true
// full set for an all-repos install. Paginated; installs with hundreds of repos are common here.
func listInstallationRepos(installationID int64) ([]string, error) {
	jwt, err := appJWT()
	if err != nil {
		return nil, err
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := ghDo(jwt, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", installationID), nil, &tok); err != nil {
		return nil, fmt.Errorf("mint installation token: %w", err)
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("empty installation token")
	}
	var all []string
	for page := 1; ; page++ {
		var resp struct {
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		if err := ghDo(tok.Token, "GET", fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), nil, &resp); err != nil {
			return nil, fmt.Errorf("list installation repos (page %d): %w", page, err)
		}
		for _, r := range resp.Repositories {
			if r.FullName != "" {
				all = append(all, r.FullName)
			}
		}
		if len(resp.Repositories) < 100 {
			break
		}
	}
	return all, nil
}

// appCreateIssue files an issue on repo as the App and returns its html_url. Labels are best-effort
// (ensured then applied; if labeling fails the issue is still created unlabeled — the important part).
func appCreateIssue(repo, title, body string, labels []string) (string, error) {
	tok, err := appInstallationToken(repo)
	if err != nil {
		return "", err
	}
	for _, l := range labels { // idempotent ensure-exists; ignores "already_exists"
		ghDo(tok, "POST", "/repos/"+repo+"/labels", map[string]any{"name": l, "color": "d4c5f9"}, nil)
	}
	mk := func(withLabels bool) (string, error) {
		p := map[string]any{"title": title, "body": body}
		if withLabels && len(labels) > 0 {
			p["labels"] = labels
		}
		var res struct {
			HTMLURL string `json:"html_url"`
		}
		if err := ghDo(tok, "POST", "/repos/"+repo+"/issues", p, &res); err != nil {
			return "", err
		}
		return res.HTMLURL, nil
	}
	if url, err := mk(true); err == nil {
		return url, nil
	}
	return mk(false) // retry unlabeled so a label hiccup never loses the issue
}
