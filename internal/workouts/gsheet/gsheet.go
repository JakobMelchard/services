// Package gsheet is a native Go client for the Google Sheets v4 API.
// Its token store:
//
//	~/.config/gsheet/client.json  (OAuth client id/secret)
//	~/.config/gsheet/tokens.json  (per-account access/refresh tokens)
//
// Tokens refresh transparently. Login (auth.go) runs the PKCE loopback flow;
// a service-account key (sa.go) replaces the token store entirely.
package gsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	TokenURL = "https://oauth2.googleapis.com/token"
	API      = "https://sheets.googleapis.com/v4/spreadsheets"
	Drive    = "https://www.googleapis.com/drive/v3/files"
	Scope    = "openid email https://www.googleapis.com/auth/spreadsheets https://www.googleapis.com/auth/drive.readonly"
)

// Client talks to the Sheets API for one spreadsheet. It is safe for
// concurrent use.
type Client struct {
	SheetID string
	Account string // empty = configured default
	HTTP    *http.Client

	configDir string // defaults to ~/.config/gsheet

	mu                 sync.Mutex // guards token refresh and the fields below
	serviceAccountJSON []byte
	saToken            string
	saExpires          time.Time
}

type Option func(*Client)

func WithConfigDir(dir string) Option { return func(c *Client) { c.configDir = dir } }
func WithAccount(acc string) Option   { return func(c *Client) { c.Account = acc } }

func New(sheetID string, opts ...Option) *Client {
	if i := strings.Index(sheetID, "/spreadsheets/d/"); i != -1 {
		rest := sheetID[i+len("/spreadsheets/d/"):]
		if j := strings.IndexAny(rest, "/?"); j != -1 {
			rest = rest[:j]
		}
		sheetID = rest
	}
	c := &Client{SheetID: sheetID, HTTP: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) configPath(name string) (string, error) {
	if c.configDir != "" {
		return filepath.Join(c.configDir, name), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("gsheet config dir: %w", err)
	}
	return filepath.Join(home, ".config", "gsheet", name), nil
}

func loadJSON[T any](path string) (*T, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

type clientCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// creds returns OAuth client credentials: env vars win, else client.json.
func (c *Client) creds() (id, secret string, err error) {
	id, secret = os.Getenv("GSHEET_CLIENT_ID"), os.Getenv("GSHEET_CLIENT_SECRET")
	if id != "" && secret != "" {
		return id, secret, nil
	}
	path, err := c.configPath("client.json")
	if err != nil {
		return "", "", err
	}
	cfg, err := loadJSON[clientCreds](path)
	if err != nil {
		return "", "", fmt.Errorf("no OAuth client configured: %w (run `gsheet auth setup` or set GSHEET_CLIENT_ID/SECRET)", err)
	}
	return cfg.ClientID, cfg.ClientSecret, nil
}

type token struct {
	AccessToken  string  `json:"access_token"`
	ExpiresAt    float64 `json:"expires_at"`
	RefreshToken string  `json:"refresh_token"`
}

type accounts struct {
	Accounts map[string]*token `json:"accounts"`
	Default  string            `json:"default"`
	fromEnv  bool
}

func (c *Client) loadAccounts() (*accounts, error) {
	if envJSON := os.Getenv("GSHEET_TOKENS_JSON"); envJSON != "" {
		var acc accounts
		if err := json.Unmarshal([]byte(envJSON), &acc); err != nil {
			return nil, fmt.Errorf("GSHEET_TOKENS_JSON: %w", err)
		}
		if len(acc.Accounts) == 0 {
			return nil, fmt.Errorf("GSHEET_TOKENS_JSON: no accounts")
		}
		acc.fromEnv = true
		return &acc, nil
	}
	path, err := c.configPath("tokens.json")
	if err != nil {
		return nil, err
	}
	acc, err := loadJSON[accounts](path)
	if err != nil {
		return nil, fmt.Errorf("no tokens: %w (run `gsheet auth login`, or set GSHEET_TOKENS_JSON / GSHEET_SERVICE_PRINCIPAL_JSON)", err)
	}
	if len(acc.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured (run `gsheet auth login`)")
	}
	return acc, nil
}

func (c *Client) saveAccounts(acc *accounts) error {
	if acc.fromEnv {
		return nil
	}
	b, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	path, err := c.configPath("tokens.json")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// AccessToken returns a valid token, refreshing from the store if expired.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.saJSON()) > 0 {
		return c.serviceAccountToken(ctx)
	}
	acc, err := c.loadAccounts()
	if err != nil {
		return "", err
	}
	acct := c.Account
	if acct == "" {
		acct = acc.Default
	}
	if acct == "" {
		return "", fmt.Errorf("no default account (run `gsheet auth default <email>`)")
	}
	tok, ok := acc.Accounts[acct]
	if !ok {
		return "", fmt.Errorf("no token for %s (run `gsheet auth login`)", acct)
	}
	if tok.AccessToken != "" && tok.ExpiresAt > float64(time.Now().Unix())+60 {
		return tok.AccessToken, nil
	}
	id, secret, err := c.creds()
	if err != nil {
		return "", err
	}
	form := url.Values{
		"client_id":     {id},
		"client_secret": {secret},
		"refresh_token": {tok.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Refresh     string `json:"refresh_token"`
	}
	if err := c.tokenPost(ctx, TokenURL, form, &out); err != nil {
		return "", fmt.Errorf("token refresh: %w", err)
	}
	tok.AccessToken = out.AccessToken
	tok.ExpiresAt = float64(time.Now().Unix()) + float64(out.ExpiresIn)
	if out.Refresh != "" {
		tok.RefreshToken = out.Refresh
	}
	if err := c.saveAccounts(acc); err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

// tokenPost posts form to an OAuth token endpoint and decodes the JSON reply
// into out. A non-200 reply is a plain error, not an *APIError: callers map
// APIError statuses to Sheets meanings (400 is a missing tab).
func (c *Client) tokenPost(ctx context.Context, uri string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", uri, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

// request does an authenticated JSON request. Body nil = GET. Cancelling ctx
// ends the request and the wait between retries.
func (c *Client) request(ctx context.Context, method, rawurl string, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	// Quota errors (429) are retried with exponential backoff, as Google
	// recommends: 1s, 2s, 4s, 8s, 16s, then the error is returned.
	for wait := time.Second; ; wait *= 2 {
		b, status, err := c.do(ctx, method, rawurl, payload, body != nil)
		if status != http.StatusTooManyRequests || wait > backoff {
			return b, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("quota backoff: %w", ctx.Err())
		case <-time.After(wait):
		}
	}
}

// backoff is the longest single wait before a 429 is returned as an error;
// tests shorten it.
var backoff = 16 * time.Second

func (c *Client) do(ctx context.Context, method, rawurl string, payload []byte, hasBody bool) ([]byte, int, error) {
	tok, err := c.AccessToken(ctx)
	if err != nil {
		return nil, 0, err
	}
	var rdr io.Reader
	if hasBody {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawurl, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read HTTP %d reply: %w", resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	return b, resp.StatusCode, nil
}

// APIError is a non-2xx reply from a Google API; match it with errors.As.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// Range builds "'Title'!A1:X1000".
func Range(title string, cols, rows int) string {
	return RangeAt(title, 1, cols, rows)
}

// RangeAt builds "'Title'!A{startRow}:X{endRow}".
func RangeAt(title string, startRow, cols, rows int) string {
	return fmt.Sprintf("%s!A%d:%s%d", quote(title), startRow, colLetter(cols), startRow+rows-1)
}

// Columns builds "'Title'!A1:X": every row of the first cols columns.
func Columns(title string, cols int) string {
	return fmt.Sprintf("%s!A1:%s", quote(title), colLetter(cols))
}

// quote wraps a tab title in single quotes, doubling inner ones, so titles
// with spaces, quotes or cell-like names ("W41") parse in A1 notation.
func quote(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'"
}

func colLetter(n int) string {
	s := ""
	for n > 0 {
		rem := (n - 1) % 26
		n = (n - 1) / 26
		s = string(rune('A'+rem)) + s
	}
	return s
}

// Read fetches a tab's grid as [][]string.
func (c *Client) Read(ctx context.Context, title string, cols, rows int) ([][]string, error) {
	return c.ReadRange(ctx, Range(title, cols, rows))
}

// ReadRange fetches an arbitrary range as [][]string.
func (c *Client) ReadRange(ctx context.Context, rng string) ([][]string, error) {
	u := API + "/" + url.PathEscape(c.SheetID) + "/values/" + url.PathEscape(rng)
	b, err := c.request(ctx, "GET", u+"?valueRenderOption=FORMATTED_VALUE", nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Values [][]any `json:"values"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(data.Values))
	for _, row := range data.Values {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = cellString(v)
		}
		out = append(out, cells)
	}
	return out, nil
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// Write replaces a range's values.
func (c *Client) Write(ctx context.Context, rng string, values [][]string) error {
	vals := make([][]any, len(values))
	for i, r := range values {
		row := make([]any, len(r))
		for j, v := range r {
			row[j] = v
		}
		vals[i] = row
	}
	body := map[string]any{
		"valueInputOption": "RAW",
		"data":             []map[string]any{{"range": rng, "values": vals}},
	}
	_, err := c.request(ctx, "POST", API+"/"+url.PathEscape(c.SheetID)+"/values:batchUpdate", body)
	return err
}

// Clear empties a range's values, keeping formatting and notes.
func (c *Client) Clear(ctx context.Context, rng string) error {
	_, err := c.request(ctx, "POST", API+"/"+url.PathEscape(c.SheetID)+"/values/"+url.PathEscape(rng)+":clear", map[string]any{})
	return err
}

// batchUpdate sends a list of sheet mutation requests.
func (c *Client) batchUpdate(ctx context.Context, requests []any) error {
	_, err := c.request(ctx, "POST", API+"/"+url.PathEscape(c.SheetID)+":batchUpdate", map[string]any{"requests": requests})
	return err
}

// Spreadsheet is a Drive file listing result.
type Spreadsheet struct {
	Name string
	ID   string
}

// List returns accessible spreadsheets via the Drive API.
func (c *Client) List(ctx context.Context) ([]Spreadsheet, error) {
	q := url.QueryEscape("mimeType='application/vnd.google-apps.spreadsheet'")
	b, err := c.request(ctx, "GET", Drive+"?q="+q+"&fields=files(id,name)&pageSize=100", nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	out := make([]Spreadsheet, 0, len(data.Files))
	for _, f := range data.Files {
		out = append(out, Spreadsheet{Name: f.Name, ID: f.ID})
	}
	return out, nil
}
