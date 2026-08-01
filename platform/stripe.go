package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// handleCheckout creates a Stripe Checkout session (subscription mode, the €20 price) and
// returns its URL — what `mago subscribe` prints.
func (s *server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	u := s.store.GetByID(uid)
	if u == nil {
		httpErr(w, 404, "not found")
		return
	}
	if s.stripeKey == "" || s.priceID == "" {
		httpErr(w, 503, "billing not configured")
		return
	}
	cus := u.StripeCustomer
	if cus == "" {
		c, err := stripeCreateCustomer(s.stripeKey, u.Email, fmt.Sprint(u.ID))
		if err != nil {
			log.Printf("stripe customer: %v", err)
			httpErr(w, 500, "billing setup failed")
			return
		}
		cus = c
		s.store.Update(uid, func(u *User) { u.StripeCustomer = cus })
	}
	link, err := stripeCheckout(s.stripeKey, cus, s.priceID, s.appURL, fmt.Sprint(uid))
	if err != nil {
		log.Printf("stripe checkout: %v", err)
		httpErr(w, 500, "checkout failed")
		return
	}
	writeJSON(w, 200, map[string]string{"url": link})
}

// handlePortal creates a Stripe Billing Portal session so the CEO can manage the subscription
// (update card, view invoices, cancel) — what `mago billing` prints. Requires an existing
// customer (i.e. they've subscribed at least once).
func (s *server) handlePortal(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	u := s.store.GetByID(uid)
	if u == nil {
		httpErr(w, 404, "not found")
		return
	}
	if s.stripeKey == "" {
		httpErr(w, 503, "billing not configured")
		return
	}
	if u.StripeCustomer == "" {
		httpErr(w, 409, "no billing account yet — run `mago subscribe` first")
		return
	}
	link, err := stripeBillingPortal(s.stripeKey, u.StripeCustomer, s.appURL+"/subscribed?portal=1")
	if err != nil {
		log.Printf("stripe portal: %v", err)
		httpErr(w, 500, "could not open billing portal")
		return
	}
	writeJSON(w, 200, map[string]string{"url": link})
}

func stripeBillingPortal(key, customer, returnURL string) (string, error) {
	f := url.Values{}
	f.Set("customer", customer)
	f.Set("return_url", returnURL)
	var out struct {
		URL string `json:"url"`
	}
	if err := stripePost(key, "billing_portal/sessions", f, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

func stripeCreateCustomer(key, email, uid string) (string, error) {
	f := url.Values{}
	f.Set("email", email)
	f.Set("metadata[user_id]", uid)
	var out struct {
		ID string `json:"id"`
	}
	return out.ID, stripePost(key, "customers", f, &out)
}

func stripeCheckout(key, customer, price, appURL, uid string) (string, error) {
	f := url.Values{}
	f.Set("mode", "subscription")
	f.Set("customer", customer)
	f.Set("line_items[0][price]", price)
	f.Set("line_items[0][quantity]", "1")
	f.Set("success_url", appURL+"/subscribed?ok=1")
	f.Set("cancel_url", appURL+"/subscribed?cancelled=1")
	f.Set("metadata[user_id]", uid)
	f.Set("metadata[plan]", "mago")
	f.Set("subscription_data[metadata][user_id]", uid)
	f.Set("allow_promotion_codes", "true")
	var out struct {
		URL string `json:"url"`
	}
	if err := stripePost(key, "checkout/sessions", f, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

func stripePost(key, path string, form url.Values, out any) error {
	req, _ := http.NewRequest("POST", "https://api.stripe.com/v1/"+path, strings.NewReader(form.Encode()))
	req.SetBasicAuth(key, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("stripe %s -> %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

func stripeGet(key, path string, q url.Values, out any) error {
	url := "https://api.stripe.com/v1/" + path
	if len(q) > 0 {
		url += "?" + q.Encode()
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(key, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("stripe %s -> %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

// stripeCustomerByEmail returns an existing Stripe customer id for the given email,
// or an empty string if none exists. This prevents duplicate customer records when
// a user re-checkouts with the same email.
func stripeCustomerByEmail(key, email string) (string, error) {
	if email == "" {
		return "", nil
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	q := url.Values{}
	q.Set("email", email)
	q.Set("limit", "1")
	if err := stripeGet(key, "customers", q, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", nil
	}
	return out.Data[0].ID, nil
}

// handleWebhook verifies the Stripe signature, dedups, and activates/downgrades the user.
func (s *server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if !verifyStripeSig(s.webhookSecret, r.Header.Get("Stripe-Signature"), body) {
		httpErr(w, 400, "bad signature")
		return
	}
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &ev) != nil {
		httpErr(w, 400, "bad json")
		return
	}
	if !s.store.FirstEvent(ev.ID) { // dedup
		w.WriteHeader(200)
		return
	}
	switch ev.Type {
	case "checkout.session.completed":
		var o struct {
			Customer     string            `json:"customer"`
			Subscription string            `json:"subscription"`
			Metadata     map[string]string `json:"metadata"`
		}
		json.Unmarshal(ev.Data.Object, &o)
		if uid := atoi(o.Metadata["user_id"]); uid > 0 {
			s.store.Update(uid, func(u *User) {
				u.Plan, u.StripeCustomer, u.StripeSub = "mago", o.Customer, o.Subscription
				if u.LicenseKey == "" {
					u.LicenseKey = genLicense()
				}
			})
			log.Printf("stripe: user %d activated (mago)", uid)
			s.store.LogEvent("subscribed", uid, "→ mago €20/mo")
		}
	case "customer.subscription.deleted":
		var o struct {
			Metadata map[string]string `json:"metadata"`
		}
		json.Unmarshal(ev.Data.Object, &o)
		if uid := atoi(o.Metadata["user_id"]); uid > 0 {
			s.store.Update(uid, func(u *User) { u.Plan = "free" })
			log.Printf("stripe: user %d downgraded (free)", uid)
			s.store.LogEvent("canceled", uid, "→ free")
		}
	}
	w.WriteHeader(200)
}

// verifyStripeSig checks Stripe's `Stripe-Signature: t=<ts>,v1=<hmac>` header.
func verifyStripeSig(secret, header string, body []byte) bool {
	if secret == "" {
		return false
	}
	var ts, v1 string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "t":
			ts = v
		case "v1":
			v1 = v
		}
	}
	if ts == "" || v1 == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	return hmac.Equal([]byte(v1), []byte(hex.EncodeToString(mac.Sum(nil))))
}
