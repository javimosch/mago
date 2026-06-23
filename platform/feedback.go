package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// feedback.go is the operator-feedback ingress: `mago feedback` POSTs here. We record it as an event
// (so it shows in `mago-platform activity`) AND file a triage item on the product repo — a GitHub
// issue labeled `feedback`, deliberately NOT `mago`, so the dogfood worker never auto-implements raw
// operator feedback. A human / the planner promotes the real ones into the roadmap.

func (s *server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	var in struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Version string `json:"version"`
		OS      string `json:"os"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" {
		httpErr(w, 400, "message required")
		return
	}
	if in.Type == "" {
		in.Type = "feedback"
	}

	who := "account " + strconv.FormatInt(uid, 10)
	if u := s.store.GetByID(uid); u != nil && u.Email != "" {
		who = u.Email
	}
	s.store.LogEvent("feedback", uid, "["+in.Type+"] "+clip(in.Message, 160))

	// File a triage issue on the product repo (best-effort — feedback is already recorded above, so a
	// GitHub hiccup never loses it). Label `feedback` only, never `mago`, so it isn't auto-implemented.
	issueURL := ""
	if repo := env("MAGO_FEEDBACK_REPO", ""); repo != "" {
		title := "[feedback:" + in.Type + "] " + clip(in.Message, 70)
		body := fmt.Sprintf("Reported by operator **%s** via `mago feedback`.\n\n- type: `%s`\n- mago: `%s` (%s)\n\n---\n\n%s",
			who, in.Type, orStr(in.Version, "?"), orStr(in.OS, "?"), in.Message)
		var res struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
		}
		if err := ghAPI("POST", "/repos/"+repo+"/issues", map[string]any{"title": title, "body": body}, &res); err != nil {
			log.Printf("feedback: issue create failed on %s: %v", repo, err)
		} else {
			issueURL = res.HTMLURL
			// Best-effort label (separate call so a missing label never blocks issue creation).
			if res.Number > 0 {
				ghAPI("POST", fmt.Sprintf("/repos/%s/issues/%d/labels", repo, res.Number),
					map[string]any{"labels": []string{"feedback"}}, nil)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "issue_url": issueURL})
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func orStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
