package gsheet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"

// SetupClient saves the desktop OAuth client credentials.
func (c *Client) SetupClient(clientID, clientSecret string) error {
	b, _ := json.MarshalIndent(clientCreds{ClientID: clientID, ClientSecret: clientSecret}, "", "  ")
	path, err := c.configPath("client.json")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func pkce() (verifier, challenge string) {
	b := make([]byte, 48)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func idTokenEmail(idToken string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("malformed id_token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	return claims.Email, nil
}

type loginResult struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

// Login runs the OAuth PKCE loopback flow and stores a token for the
// account it returns. The URL to authorize (and any browser error) goes to w.
func (c *Client) Login(w io.Writer, openBrowser bool) (string, error) {
	id, secret, err := c.creds()
	if err != nil {
		return "", err
	}
	verifier, challenge := pkce()
	state := rand.Text()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
	params := url.Values{
		"client_id":             {id},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	authURL := AuthURL + "?" + params.Encode()
	fmt.Fprintf(w, "Open this URL in a browser and authorize:\n  %s\n", authURL)
	if openBrowser {
		open := "xdg-open"
		if runtime.GOOS == "darwin" {
			open = "open"
		}
		if err := exec.Command(open, authURL).Start(); err != nil {
			fmt.Fprintln(w, "(could not auto-open browser; open the URL manually)")
		}
	}
	code, err := awaitCode(ln, state)
	if err != nil {
		return "", err
	}
	var out loginResult
	if err := c.tokenPost(context.Background(), TokenURL, url.Values{
		"client_id":     {id},
		"client_secret": {secret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	}, &out); err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	email, err := idTokenEmail(out.IDToken)
	if err != nil {
		return "", err
	}
	acc, err := c.loadAccounts()
	if err != nil {
		acc = &accounts{Accounts: map[string]*token{}}
	}
	if acc.Accounts == nil {
		acc.Accounts = map[string]*token{}
	}
	acc.Accounts[email] = &token{
		AccessToken:  out.AccessToken,
		ExpiresAt:    float64(time.Now().Unix()) + float64(out.ExpiresIn),
		RefreshToken: out.RefreshToken,
	}
	if acc.Default == "" {
		acc.Default = email
	}
	return email, c.saveAccounts(acc)
}

// awaitCode serves the OAuth redirect on ln until it brings the authorization
// code for state, an error, or five minutes pass.
func awaitCode(ln net.Listener, state string) (string, error) {
	codeCh, failCh := make(chan string, 1), make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", 400)
			return
		}
		if e := q.Get("error"); e != "" {
			failCh <- e
			fmt.Fprintln(w, "Authorization failed.")
			return
		}
		codeCh <- q.Get("code")
		fmt.Fprintln(w, "Authorization complete. Close this window.")
	})}
	go srv.Serve(ln)
	defer srv.Close()
	select {
	case code := <-codeCh:
		return code, nil
	case e := <-failCh:
		return "", fmt.Errorf("authorization failed: %s", e)
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("authorization timed out")
	}
}

// Accounts lists configured account emails, default marked.
func (c *Client) Accounts() ([]string, string, error) {
	acc, err := c.loadAccounts()
	if err != nil {
		return nil, "", err
	}
	emails := make([]string, 0, len(acc.Accounts))
	for e := range acc.Accounts {
		emails = append(emails, e)
	}
	return emails, acc.Default, nil
}

// Revoke removes an account's token.
func (c *Client) Revoke(email string) error {
	acc, err := c.loadAccounts()
	if err != nil {
		return err
	}
	if _, ok := acc.Accounts[email]; !ok {
		return fmt.Errorf("no token for %s", email)
	}
	delete(acc.Accounts, email)
	if acc.Default == email {
		acc.Default = ""
		for e := range acc.Accounts {
			acc.Default = e
			break
		}
	}
	return c.saveAccounts(acc)
}

// SetDefault marks the default account.
func (c *Client) SetDefault(email string) error {
	acc, err := c.loadAccounts()
	if err != nil {
		return err
	}
	if _, ok := acc.Accounts[email]; !ok {
		return fmt.Errorf("no token for %s", email)
	}
	acc.Default = email
	return c.saveAccounts(acc)
}
