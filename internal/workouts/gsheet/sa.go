package gsheet

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"time"
)

// saScope is the service-account scope: no openid/email (user-account scopes).
const saScope = "https://www.googleapis.com/auth/spreadsheets https://www.googleapis.com/auth/drive.readonly"

type saCreds struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// WithServiceAccount configures auth via a Google service-account key JSON
// (JWT bearer flow, RSA). Env var GSHEET_SERVICE_PRINCIPAL_JSON wins over this option.
func WithServiceAccount(key []byte) Option {
	return func(c *Client) { c.serviceAccountJSON = key }
}

func (c *Client) saJSON() []byte {
	if env := os.Getenv("GSHEET_SERVICE_PRINCIPAL_JSON"); env != "" {
		return []byte(env)
	}
	return c.serviceAccountJSON
}

// serviceAccountToken returns a fresh access token, signing a JWT assertion
// with the service-account key. Caches until ~1 min before expiry.
func (c *Client) serviceAccountToken(ctx context.Context) (string, error) {
	if c.saToken != "" && time.Until(c.saExpires) > time.Minute {
		return c.saToken, nil
	}
	b := c.saJSON()
	if len(b) == 0 {
		return "", nil
	}
	var sa saCreds
	if err := json.Unmarshal(b, &sa); err != nil {
		return "", fmt.Errorf("service account: %w", err)
	}
	key, err := rsaKey([]byte(sa.PrivateKey))
	if err != nil {
		return "", err
	}
	uri := sa.TokenURI
	if uri == "" {
		uri = TokenURL
	}
	now := time.Now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   sa.ClientEmail,
		"scope": saScope,
		"aud":   uri,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	h, _ := json.Marshal(header)
	cld, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(cld)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	assert := unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)

	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assert}}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.tokenPost(ctx, uri, form, &out); err != nil {
		return "", fmt.Errorf("service account token: %w", err)
	}
	c.saToken, c.saExpires = out.AccessToken, now.Add(time.Duration(out.ExpiresIn)*time.Second)
	return out.AccessToken, nil
}

func rsaKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("service account: no PEM private key")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("service account: unsupported private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("service account: RSA key required, got %T", k)
	}
	return rk, nil
}
