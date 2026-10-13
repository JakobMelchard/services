package gsheet_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// fakeTransport rewrites Google API hosts to a local HTTP test server.
type fakeTransport struct {
	target string
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	switch {
	case strings.Contains(u.Host, "sheets.googleapis.com"):
		u.Host = f.target
	case strings.Contains(u.Host, "oauth2.googleapis.com"):
		u.Host = f.target
	case strings.Contains(u.Host, "www.googleapis.com"):
		u.Host = f.target
	default:
		return nil, net.ErrClosed
	}
	u.Scheme = "http"
	req.URL = &u
	req.Host = f.target
	return http.DefaultTransport.RoundTrip(req)
}

type state struct {
	sheets    map[string]map[string][][]string
	refreshed int
}

func TestEndToEnd(t *testing.T) {
	st := &state{sheets: map[string]map[string][][]string{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		st.refreshed++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-" + time.Now().String(),
			"expires_in":    3600,
			"refresh_token": "rt",
			"id_token":      "x.y", // not used here
		})
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v4/spreadsheets/")
		id := path
		if i := strings.Index(id, "/"); i != -1 {
			id = id[:i]
		}
		if r.Method == "GET" && strings.Contains(r.URL.String(), "?fields=sheets.properties") {
			// list tabs
			tabs := make([]any, 0, len(st.sheets[id]))
			for name := range st.sheets[id] {
				tabs = append(tabs, map[string]any{"properties": map[string]any{"title": name, "sheetId": 1}})
			}
			json.NewEncoder(w).Encode(map[string]any{"sheets": tabs})
			return
		}
		if strings.Contains(path, "/values:batchUpdate") {
			var body struct {
				Data []struct {
					Range  string     `json:"range"`
					Values [][]string `json:"values"`
				} `json:"data"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			for _, d := range body.Data {
				tab := strings.Trim(d.Range[:strings.Index(d.Range, "!")], "'")
				if st.sheets[id] == nil {
					st.sheets[id] = map[string][][]string{}
				}
				st.sheets[id][tab] = d.Values
			}
			w.WriteHeader(200)
			w.Write([]byte("{}"))
			return
		}
		if strings.Contains(path, ":batchUpdate") {
			var body struct {
				Requests []map[string]any `json:"requests"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Requests) == 0 {
				w.WriteHeader(200)
				w.Write([]byte("{}"))
				return
			}
			if a, ok := body.Requests[0]["addSheet"]; ok {
				props := a.(map[string]any)["properties"].(map[string]any)
				title := props["title"].(string)
				if st.sheets[id] == nil {
					st.sheets[id] = map[string][][]string{}
				}
				if _, ok := st.sheets[id][title]; !ok {
					st.sheets[id][title] = [][]string{}
				}
				json.NewEncoder(w).Encode(map[string]any{
					"replies": []any{map[string]any{"addSheet": map[string]any{"properties": map[string]any{"sheetId": 1}}}},
				})
				return
			}
			w.WriteHeader(200)
			w.Write([]byte("{}"))
			return
		}
		// values read
		if strings.Contains(path, "/values/") {
			rng := path[strings.Index(path, "/values/")+8:]
			if i := strings.Index(rng, "?"); i != -1 {
				rng = rng[:i]
			}
			rng, _ = url.PathUnescape(rng)
			tab := strings.Trim(rng[:strings.Index(rng, "!")], "'")
			if _, ok := st.sheets[id][tab]; !ok {
				st.sheets[id][tab] = [][]string{}
			}
			json.NewEncoder(w).Encode(map[string]any{"values": st.sheets[id][tab]})
			return
		}
		w.WriteHeader(404)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("404: %s %s", r.Method, r.URL.String())
		w.WriteHeader(404)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "client.json"), []byte(`{"client_id":"cid","client_secret":"cs"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(`{"accounts":{"me@x.com":{"access_token":"old","expires_at":1,"refresh_token":"rt"}},"default":"me@x.com"}`), 0o600)

	host := strings.TrimPrefix(srv.URL, "http://")
	c := gsheet.New("SPREADSHEET_1", gsheet.WithConfigDir(dir))
	c.HTTP = &http.Client{Transport: &fakeTransport{target: host}}

	// Seed a routine tab.
	st.sheets["SPREADSHEET_1"] = map[string][][]string{}
	st.sheets["SPREADSHEET_1"]["UpperA"] = [][]string{{"Exercise", "Sets", "Reps", "Weight"}, {"Deadlift", "4", "8-9", "100"}}

	ctx := t.Context()
	gid, err := c.AddTab(ctx, "Sess Test", "Exercise,Set,Reps,Weight,Done")
	if err != nil {
		t.Fatal(err)
	}
	if gid != 1 {
		t.Errorf("gid = %d", gid)
	}
	if err := c.Write(ctx, "Sess Test!A2:E2", [][]string{{"Deadlift", "1", "8-9", "100", "TRUE"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Checkbox(ctx, "Sess Test", "E", 2, 2); err != nil {
		t.Fatal(err)
	}
	grid, err := c.Read(ctx, "Sess Test", 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 1 || grid[0][0] != "Deadlift" {
		t.Errorf("read back = %v", grid)
	}
	tabs, err := c.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 {
		t.Errorf("tabs = %v", tabs)
	}
	if st.refreshed == 0 {
		t.Error("expected token refresh to run (expired token)")
	}
}

func TestServiceAccount(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	saJSON := fmt.Sprintf(`{"client_email":"svc@proj.iam.gserviceaccount.com","private_key":%q,"token_uri":"https://oauth2.googleapis.com/token"}`, pemBytes)

	var tokenCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCount++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", got)
		}
		assert := r.Form.Get("assertion")
		parts := strings.Split(assert, ".")
		if len(parts) != 3 {
			t.Fatalf("assertion = %q", assert)
		}
		header, _ := base64.RawURLEncoding.DecodeString(parts[0])
		claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		if !strings.Contains(string(header), `"alg":"RS256"`) {
			t.Errorf("header = %s", header)
		}
		if !strings.Contains(string(claims), `"iss":"svc@proj.iam.gserviceaccount.com"`) {
			t.Errorf("claims = %s", claims)
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
			t.Errorf("bad signature: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "sa-token", "expires_in": 3600})
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sa-token" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"sheets": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := gsheet.New("SPREADSHEET_1", gsheet.WithServiceAccount([]byte(saJSON)))
	c.HTTP = &http.Client{Transport: &fakeTransport{target: host}}

	if _, err := c.Tabs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tabs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if tokenCount != 1 {
		t.Errorf("token fetched %d times, want 1 (cached)", tokenCount)
	}
}

// TestConcurrentRequests shares one client across goroutines, as a server
// does; run with -race. Tokens expire at once, so every request refreshes.
func TestConcurrentRequests(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	saJSON := fmt.Sprintf(`{"client_email":"svc@x","private_key":%q}`, pemBytes)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": "sa-token", "expires_in": 0})
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sheets": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := gsheet.New("S", gsheet.WithServiceAccount([]byte(saJSON)))
	c.HTTP = &http.Client{Transport: &fakeTransport{target: strings.TrimPrefix(srv.URL, "http://")}}

	errs := make(chan error, 8)
	for range 8 {
		go func() { _, err := c.Tabs(t.Context()); errs <- err }()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"Unable to parse range: nope!A1"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tokens.json"), fmt.Appendf(nil, `{"default":"a","accounts":{"a":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix()), 0o600)
	c := gsheet.New("S", gsheet.WithConfigDir(dir))
	c.HTTP = &http.Client{Transport: &fakeTransport{target: strings.TrimPrefix(srv.URL, "http://")}}

	_, err := c.Read(t.Context(), "nope", 1, 1)
	var ae *gsheet.APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want *APIError 400", err)
	}
}

// EditRows sends the ops last first, shifted by offset, against the tab's id.
func TestEditRows(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"sheets":[{"properties":{"title":"Other","sheetId":1}},{"properties":{"title":"Log","sheetId":7}}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tokens.json"), fmt.Appendf(nil, `{"default":"a","accounts":{"a":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix()), 0o600)
	c := gsheet.New("S", gsheet.WithConfigDir(dir))
	c.HTTP = &http.Client{Transport: &fakeTransport{target: strings.TrimPrefix(srv.URL, "http://")}}

	ops := []gsheet.RowOp{{Start: 0, End: 1}, {Start: 2, End: 3}, {Start: 4, End: 6, Insert: true}}
	if err := c.EditRows(t.Context(), "Log", 10, ops); err != nil {
		t.Fatal(err)
	}
	want := `{"requests":[` +
		`{"insertDimension":{"range":{"dimension":"ROWS","endIndex":16,"sheetId":7,"startIndex":14}}},` +
		`{"deleteDimension":{"range":{"dimension":"ROWS","endIndex":13,"sheetId":7,"startIndex":12}}},` +
		`{"deleteDimension":{"range":{"dimension":"ROWS","endIndex":11,"sheetId":7,"startIndex":10}}}]}`
	if strings.TrimSpace(body) != want {
		t.Errorf("batchUpdate body:\n%s\nwant:\n%s", body, want)
	}
}
