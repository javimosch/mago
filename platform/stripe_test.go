package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

// rewriteTransport redirects requests aimed at api.stripe.com to the test server
// so the stripe helpers can be exercised without a real Stripe key.
type rewriteTransport struct {
	base *url.URL
}

func (rt *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = rt.base.Scheme
	r2.URL.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func mockStripeServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/customers" && r.Method == "GET":
			email := r.URL.Query().Get("email")
			if email == "existing@example.com" {
				json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{{"id": "cus_existing"}},
				})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
		case r.URL.Path == "/v1/customers" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{"id": "cus_new"})
		case r.URL.Path == "/v1/checkout/sessions" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]any{"url": "https://checkout.stripe.com/test"})
		case r.URL.Path == "/v1/subscriptions/sub_old" && r.Method == "DELETE":
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"sub_old","status":"canceled"}`))
		case r.URL.Path == "/v1/subscriptions/sub_missing" && r.Method == "DELETE":
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"not found"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))

	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &rewriteTransport{base: mustURL(srv.URL)}}
	t.Cleanup(func() { http.DefaultClient = old })
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestStripeCustomerByEmail(t *testing.T) {
	mockStripeServer(t)

	got, err := stripeCustomerByEmail("sk_test", "existing@example.com")
	if err != nil || got != "cus_existing" {
		t.Fatalf("existing customer: err=%v got=%q", err, got)
	}

	got, err = stripeCustomerByEmail("sk_test", "new@example.com")
	if err != nil || got != "" {
		t.Fatalf("new customer: err=%v got=%q", err, got)
	}

	got, err = stripeCustomerByEmail("sk_test", "")
	if err != nil || got != "" {
		t.Fatalf("empty email: err=%v got=%q", err, got)
	}
}

func TestStripeCreateCustomer(t *testing.T) {
	mockStripeServer(t)

	got, err := stripeCreateCustomer("sk_test", "new@example.com", "1")
	if err != nil || got != "cus_new" {
		t.Fatalf("create customer: err=%v got=%q", err, got)
	}
}

func TestStripeCheckout(t *testing.T) {
	mockStripeServer(t)

	got, err := stripeCheckout("sk_test", "cus_existing", "price_1", "https://app.example", "1")
	if err != nil || got != "https://checkout.stripe.com/test" {
		t.Fatalf("checkout session: err=%v got=%q", err, got)
	}
}

func TestStripeCancelSubscription(t *testing.T) {
	mockStripeServer(t)

	if err := stripeCancelSubscription("sk_test", "sub_old"); err != nil {
		t.Fatalf("cancel existing sub: %v", err)
	}

	// A missing subscription (404) should be treated as already gone.
	if err := stripeCancelSubscription("sk_test", "sub_missing"); err != nil {
		t.Fatalf("cancel missing sub: %v", err)
	}

	// Empty subscription is a no-op.
	if err := stripeCancelSubscription("sk_test", ""); err != nil {
		t.Fatalf("cancel empty sub: %v", err)
	}
}

func TestHandleCheckoutReusesCustomerByEmail(t *testing.T) {
	mockStripeServer(t)

	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("existing@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	s := &server{
		store:     st,
		stripeKey: "sk_test",
		priceID:   "price_1",
		appURL:    "https://app.example",
		jwtSecret: "secret",
	}

	tok := jwtSign("secret", u.ID, u.Email)
	req := httptest.NewRequest("POST", "/api/checkout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	s.handleCheckout(w, req)

	if w.Code != 200 {
		t.Fatalf("checkout status=%d body=%s", w.Code, w.Body.String())
	}

	var out struct{ URL string }
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.URL != "https://checkout.stripe.com/test" {
		t.Fatalf("checkout url=%q", out.URL)
	}

	fresh := st.GetByID(u.ID)
	if fresh.StripeCustomer != "cus_existing" {
		t.Fatalf("customer not reused, got %q", fresh.StripeCustomer)
	}
}

func TestHandleCheckoutFallsBackToCreatingCustomer(t *testing.T) {
	mockStripeServer(t)

	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("new@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}

	s := &server{
		store:     st,
		stripeKey: "sk_test",
		priceID:   "price_1",
		appURL:    "https://app.example",
		jwtSecret: "secret",
	}

	tok := jwtSign("secret", u.ID, u.Email)
	req := httptest.NewRequest("POST", "/api/checkout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	s.handleCheckout(w, req)

	if w.Code != 200 {
		t.Fatalf("checkout status=%d body=%s", w.Code, w.Body.String())
	}

	fresh := st.GetByID(u.ID)
	if fresh.StripeCustomer != "cus_new" {
		t.Fatalf("new customer not created, got %q", fresh.StripeCustomer)
	}

	var out struct{ URL string }
	json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&out)
	if out.URL != "https://checkout.stripe.com/test" {
		t.Fatalf("checkout url=%q", out.URL)
	}
}
