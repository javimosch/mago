package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// manifestTestTransport redirects requests aimed at https://api.github.com to a
// local httptest server so convertManifest can be exercised without network access.
type manifestTestTransport struct {
	srv *httptest.Server
}

func (t *manifestTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	srvURL, err := url.Parse(t.srv.URL)
	if err != nil {
		return nil, err
	}
	r := req.Clone(req.Context())
	r.URL.Scheme = srvURL.Scheme
	r.URL.Host = srvURL.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestConvertManifest_Success(t *testing.T) {
	want := appConversion{
		ID:            123,
		Slug:          "mago",
		ClientID:      "Iv1.client",
		ClientSecret:  "secret",
		WebhookSecret: "whsec",
		PEM:           "-----BEGIN RSA PRIVATE KEY-----\n-----END RSA PRIVATE KEY-----\n",
		HTMLURL:       "https://github.com/apps/mago",
	}
	want.Owner.Login = "acme"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/app-manifests/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":123,"slug":"mago","client_id":"Iv1.client","client_secret":"secret","webhook_secret":"whsec","pem":"-----BEGIN RSA PRIVATE KEY-----\\n-----END RSA PRIVATE KEY-----\\n","html_url":"https://github.com/apps/mago","owner":{"login":"acme"}}`)
	}))
	defer ts.Close()

	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &manifestTestTransport{srv: ts}}
	t.Cleanup(func() { http.DefaultClient = old })

	got, err := convertManifest("test-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID || got.Slug != want.Slug || got.ClientID != want.ClientID ||
		got.ClientSecret != want.ClientSecret || got.WebhookSecret != want.WebhookSecret ||
		got.HTMLURL != want.HTMLURL || got.Owner.Login != want.Owner.Login {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestConvertManifest_GitHubError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid manifest", http.StatusBadRequest)
	}))
	defer ts.Close()

	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &manifestTestTransport{srv: ts}}
	t.Cleanup(func() { http.DefaultClient = old })

	_, err := convertManifest("bad-code")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "400") {
		t.Errorf("error should contain status code, got: %q", msg)
	}
	if !strings.Contains(msg, "invalid manifest") {
		t.Errorf("error should contain response body, got: %q", msg)
	}
}

func TestConvertManifest_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{not json`))
	}))
	defer ts.Close()

	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &manifestTestTransport{srv: ts}}
	t.Cleanup(func() { http.DefaultClient = old })

	_, err := convertManifest("code")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
