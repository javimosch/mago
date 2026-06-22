package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// relay.go is the worker side of the platform's GitHub-webhook relay (docs/SAAS.md phase 4).
// Instead of exposing a public webhook URL (a tunnel), the worker dials OUT to the platform
// and holds a streaming connection; the platform pushes matching repo events down it. Each
// event is fed through the SAME classifyEvent -> signal path that the local listener uses.

// workerID identifies this worker to the platform so an account can run several at once (each
// serving its own repos). Defaults to the hostname; override with MAGO_WORKER_ID.
func workerID() string {
	if id := strings.TrimSpace(os.Getenv("MAGO_WORKER_ID")); id != "" {
		return id
	}
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "worker"
}

// runRelay connects to the platform and feeds relayed GitHub events into the worker. It blocks,
// reconnecting with backoff until ctx is cancelled. License key + platform URL come from the
// CLI config (~/.mago/config.json); repos come from the company.
func runRelay(ctx context.Context, w *eventWorker, cfg *cliConfig, repos []string) {
	if cfg.LicenseKey == "" {
		fmt.Fprintln(os.Stderr, "[relay] no license key in ~/.mago/config.json — run `mago account status` after subscribing; relay disabled")
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := streamRelay(ctx, w, cfg, repos)
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "[relay] disconnected (%v); reconnecting in %s\n", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// streamRelay holds one connection: it reads newline-delimited JSON events until the stream
// ends or ctx is cancelled. A clean read resets the caller's backoff via a nil-ish error.
func streamRelay(ctx context.Context, w *eventWorker, cfg *cliConfig, repos []string) error {
	q := url.Values{}
	q.Set("token", cfg.LicenseKey)
	q.Set("repos", strings.Join(repos, ","))
	q.Set("worker", workerID()) // identifies this worker so the platform keeps several per account
	u := strings.TrimRight(cfg.PlatformURL, "/") + "/ws/worker?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// 401/403 => bad/lapsed license: surface clearly (don't tight-loop silently).
		return fmt.Errorf("platform refused relay (HTTP %d)", resp.StatusCode)
	}
	fmt.Fprintf(os.Stderr, "[relay] connected to %s for repos %v\n", cfg.PlatformURL, repos)

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20) // webhook payloads can be large
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg struct {
			Event string          `json:"event"`
			Body  json.RawMessage `json:"body"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		switch msg.Event {
		case "ping", "ready": // keepalive / handshake — nothing to do
			continue
		}
		if ev, wake := classifyEvent(msg.Event, msg.Body); wake {
			fmt.Fprintf(os.Stderr, "[relay] %s -> %s\n", msg.Event, ev.reason)
			w.signal(ev)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("stream closed")
}
