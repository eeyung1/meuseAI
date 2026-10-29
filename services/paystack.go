package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	Tier1AmountKobo  int64 = 500000
	Tier2AmountKobo  int64 = 2500000
	PaystackCurrency       = "NGN"
)

// A completed four-lesson course earns 1,000 XP and a 10% discount.
func Tier1DiscountPercent(xp int) int {
	if xp >= 1000 {
		return 10
	}
	return 0
}
func ValidTier1Amount(amount int64) bool {
	return amount == Tier1AmountKobo || amount == Tier1AmountKobo*90/100
}

var paystackReference = regexp.MustCompile(`^[A-Za-z0-9.=-]{1,128}$`)

func TierAmount(tier string) (int64, error) {
	switch tier {
	case "tier_1":
		return Tier1AmountKobo, nil
	case "tier_2":
		return Tier2AmountKobo, nil
	}
	return 0, errors.New("unsupported tier")
}

type CheckoutRequest struct {
	Email     string            `json:"email"`
	Amount    int64             `json:"amount"`
	Currency  string            `json:"currency"`
	Reference string            `json:"reference"`
	Metadata  map[string]string `json:"metadata"`
}
type Checkout struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
}

// Paystack owns server credentials. It never logs provider responses or secrets.
// A supplied client enables a custom transport; redirects are always disabled.
type Paystack struct {
	secret string
	client *http.Client
}

func NewPaystack(secret string, client *http.Client) (*Paystack, error) {
	if strings.TrimSpace(secret) == "" || strings.ContainsAny(secret, "\r\n") {
		return nil, errors.New("Paystack secret is required")
	}
	c := http.Client{Timeout: 10 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout <= 0 || c.Timeout > 10*time.Second {
			c.Timeout = 10 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Paystack{secret: secret, client: &c}, nil
}

func (p *Paystack) Initialize(ctx context.Context, input CheckoutRequest) (Checkout, error) {
	var zero Checkout
	a, err := mail.ParseAddress(input.Email)
	if err != nil || a.Address != input.Email || !paystackReference.MatchString(input.Reference) || input.Currency != PaystackCurrency {
		return zero, errors.New("invalid checkout request")
	}
	expected, err := TierAmount(input.Metadata["tier"])
	if err != nil || (input.Amount != expected && !(input.Metadata["tier"] == "tier_1" && ValidTier1Amount(input.Amount))) || input.Metadata["student_id"] == "" {
		return zero, errors.New("invalid checkout price or owner")
	}
	// The public API documents metadata as a stringified JSON object.
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return zero, errors.New("invalid metadata")
	}
	body, err := json.Marshal(struct {
		Email     string `json:"email"`
		Amount    int64  `json:"amount"`
		Currency  string `json:"currency"`
		Reference string `json:"reference"`
		Metadata  string `json:"metadata"`
	}{input.Email, input.Amount, input.Currency, input.Reference, string(metadata)})
	if err != nil {
		return zero, errors.New("invalid checkout request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.paystack.co/transaction/initialize", bytes.NewReader(body))
	if err != nil {
		return zero, errors.New("checkout unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+p.secret)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return zero, errors.New("checkout provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zero, errors.New("checkout provider rejected initialization")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return zero, errors.New("invalid checkout provider response")
	}
	var result struct {
		Status bool     `json:"status"`
		Data   Checkout `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Status || result.Data.Reference != input.Reference || result.Data.AccessCode == "" {
		return zero, errors.New("invalid checkout provider response")
	}
	u, err := url.Parse(result.Data.AuthorizationURL)
	if err != nil || u.Scheme != "https" || u.Host != "checkout.paystack.com" || u.User != nil {
		return zero, errors.New("invalid checkout provider URL")
	}
	return result.Data, nil
}

// ValidSignature authenticates the exact bytes received, before JSON parsing.
func (p *Paystack) ValidSignature(body []byte, signature string) bool {
	if len(signature) != sha512.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha512.New, []byte(p.secret))
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), decoded)
}
