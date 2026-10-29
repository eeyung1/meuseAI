package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type paystackTransport func(*http.Request) (*http.Response, error)

func (f paystackTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func paystackResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func paystackInput() CheckoutRequest {
	return CheckoutRequest{Email: "student@example.com", Amount: Tier1AmountKobo, Currency: "NGN", Reference: "meuse-ref", Metadata: map[string]string{"tier": "tier_1", "student_id": "student"}}
}
func TestPaystackInitialize(t *testing.T) {
	for _, tier := range []string{"tier_1", "tier_2"} {
		t.Run(tier, func(t *testing.T) {
			input := paystackInput()
			input.Metadata["tier"] = tier
			input.Amount, _ = TierAmount(tier)
			p, err := NewPaystack("test-secret", &http.Client{Transport: paystackTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.paystack.co/transaction/initialize" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("incorrect provider request")
				}
				var body struct {
					Amount    int64  `json:"amount"`
					Email     string `json:"email"`
					Currency  string `json:"currency"`
					Reference string `json:"reference"`
					Metadata  string `json:"metadata"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				var metadata map[string]string
				if err := json.Unmarshal([]byte(body.Metadata), &metadata); err != nil {
					t.Fatal(err)
				}
				if body.Amount != input.Amount || body.Email != input.Email || body.Currency != "NGN" || metadata["tier"] != tier || metadata["student_id"] != "student" {
					t.Fatal("checkout fields changed")
				}
				return paystackResponse(200, `{"status":true,"data":{"authorization_url":"https://checkout.paystack.com/code","access_code":"code","reference":"meuse-ref"}}`), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Initialize(context.Background(), input)
			if err != nil || got.Reference != input.Reference {
				t.Fatalf("initialize %+v %v", got, err)
			}
		})
	}
}
func TestPaystackRejectsInvalidProviderResponse(t *testing.T) {
	bodies := []string{`broken`, `{} fit`, `{"status":false}`, `{"status":true,"data":{"reference":"other"}}`, `{"status":true,"data":{"reference":"meuse-ref","access_code":"c","authorization_url":"http://checkout.paystack.com/c"}}`, `{"status":true,"data":{"reference":"meuse-ref","access_code":"c","authorization_url":"https://evil.example/c"}}`, strings.Repeat("x", (1<<20)+1)}
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p, _ := NewPaystack("secret", &http.Client{Transport: paystackTransport(func(*http.Request) (*http.Response, error) { return paystackResponse(200, body), nil })})
			if _, err := p.Initialize(context.Background(), paystackInput()); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	for _, status := range []int{302, 400, 401, 500} {
		p, _ := NewPaystack("secret", &http.Client{Transport: paystackTransport(func(*http.Request) (*http.Response, error) {
			return paystackResponse(status, "secret provider error"), nil
		})})
		if _, err := p.Initialize(context.Background(), paystackInput()); err == nil || strings.Contains(err.Error(), "secret provider error") {
			t.Fatal("provider error accepted/leaked")
		}
	}
}
func TestPaystackInputAndCancellation(t *testing.T) {
	called := false
	p, _ := NewPaystack("secret", &http.Client{Transport: paystackTransport(func(r *http.Request) (*http.Response, error) { called = true; return nil, r.Context().Err() })})
	for _, change := range []func(*CheckoutRequest){func(v *CheckoutRequest) { v.Amount = 1 }, func(v *CheckoutRequest) { v.Currency = "USD" }, func(v *CheckoutRequest) { v.Reference = "bad_ref" }, func(v *CheckoutRequest) { v.Email = "invalid" }, func(v *CheckoutRequest) { v.Metadata = nil }} {
		v := paystackInput()
		change(&v)
		if _, err := p.Initialize(context.Background(), v); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if called {
		t.Fatal("invalid request reached provider")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Initialize(ctx, paystackInput()); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := NewPaystack("", nil); err == nil {
		t.Fatal("empty secret accepted")
	}
	if _, err := NewPaystack("bad\nsecret", nil); err == nil {
		t.Fatal("header injection accepted")
	}
}
func TestPaystackSignature(t *testing.T) {
	p, _ := NewPaystack("secret", nil)
	body := []byte(`{"event":"charge.success"}`)
	mac := hmac.New(sha512.New, []byte("secret"))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	if !p.ValidSignature(body, signature) {
		t.Fatal("valid signature rejected")
	}
	for _, sig := range []string{"", signature[:127], strings.Repeat("g", 128), strings.Repeat("0", 128)} {
		if p.ValidSignature(body, sig) {
			t.Fatal("invalid signature accepted")
		}
	}
	if p.ValidSignature(append(body, ' '), signature) {
		t.Fatal("body tampering accepted")
	}
}

func TestTrackAmounts(t *testing.T) {
	for _, item := range []struct {
		amount int64
		tracks string
		valid  bool
	}{{500000, "video", true}, {450000, "video", true}, {1000000, "landing_pages,video", true}, {900000, "landing_pages,video", true}, {500000, "landing_pages,video", false}, {900000, "video", false}, {1000000, "video,video", false}, {500000, "unknown", false}} {
		if got := ValidTrackAmount(item.amount, item.tracks); got != item.valid {
			t.Fatalf("%d %s: %v", item.amount, item.tracks, got)
		}
	}
}
