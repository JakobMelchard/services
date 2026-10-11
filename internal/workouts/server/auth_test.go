package server

import (
	"crypto/tls"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRequireToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	tests := []struct {
		name, token, method, path, auth, cookie string
		want                                    int
	}{
		{"get without token", "s3cret", "GET", "/tabs", "", "", 204},
		{"head without token", "s3cret", "HEAD", "/", "", "", 204},
		{"delete missing", "s3cret", "DELETE", "/tabs/x", "", "", 401},
		{"delete wrong", "s3cret", "DELETE", "/tabs/x", "Bearer nope", "", 401},
		{"delete right", "s3cret", "DELETE", "/tabs/x", "Bearer s3cret", "", 204},
		{"post wrong scheme", "s3cret", "POST", "/routines", "Basic s3cret", "", 401},
		{"post right cookie", "s3cret", "POST", "/routines", "", cookieValue("s3cret"), 204},
		{"post raw token as cookie", "s3cret", "POST", "/routines", "", "s3cret", 401},
		{"post wrong cookie", "s3cret", "POST", "/routines", "", cookieValue("nope"), 401},
		{"put routine missing", "s3cret", "PUT", "/routines/x", "", "", 401},
		{"put routine right", "s3cret", "PUT", "/routines/x", "Bearer s3cret", "", 204},
		{"login exempt", "s3cret", "POST", "/login", "", "", 204},
		{"no token configured (loopback)", "", "DELETE", "/tabs/x", "", "", 204},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: cookieName, Value: tt.cookie})
			}
			w := httptest.NewRecorder()
			requireToken(tt.token, ok).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestCheckAddr(t *testing.T) {
	tests := []struct {
		addr, token string
		ok          bool
	}{
		{"127.0.0.1:8080", "", true},
		{"[::1]:8080", "", true},
		{"localhost:8080", "", true},
		{":8080", "", false},
		{"0.0.0.0:8080", "", false},
		{"192.168.1.5:8080", "", false},
		{"myhost:8080", "", false},
		{":8080", "s3cret", true},
		{"nonsense", "", false},
	}
	for _, tt := range tests {
		if err := checkAddr(tt.addr, tt.token); (err == nil) != tt.ok {
			t.Errorf("checkAddr(%q, %q) = %v, want ok=%v", tt.addr, tt.token, err, tt.ok)
		}
	}
}

func TestLogin(t *testing.T) {
	tpl := template.Must(template.New("login.html").Parse("form"))
	h := login(tpl, "s3cret")
	for _, tt := range []struct {
		token string
		want  int
	}{{"nope", 401}, {"", 401}, {"s3cret", 303}} {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"token": {tt.token}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != tt.want {
			t.Errorf("token %q: got %d, want %d", tt.token, w.Code, tt.want)
		}
		c := w.Result().Cookies()
		if tt.want == 303 && (len(c) != 1 || c[0].Value != cookieValue("s3cret") || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode) {
			t.Errorf("bad cookie: %+v", c)
		}
		if tt.want != 303 && len(c) != 0 {
			t.Errorf("cookie set on failed login: %+v", c)
		}
	}
}

func post(h http.HandlerFunc, token string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	edit(r)
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// The cookie is Secure only when the browser reached us over TLS, so plain
// http on loopback still logs in.
func TestLoginCookieSecure(t *testing.T) {
	h := login(template.Must(template.New("login.html").Parse("form")), "s3cret")
	for name, tt := range map[string]struct {
		edit func(*http.Request)
		want bool
	}{
		"plain http":      {func(r *http.Request) {}, false},
		"tls":             {func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		"proxy says tls":  {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }, true},
		"proxy says http": {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }, false},
	} {
		c := post(h, "s3cret", tt.edit).Result().Cookies()
		if len(c) != 1 || c[0].Secure != tt.want {
			t.Errorf("%s: cookies %+v, want one with Secure=%v", name, c, tt.want)
		}
	}
}

func TestLoginLocksAfterWrongTokens(t *testing.T) {
	h := login(template.Must(template.New("login.html").Parse("form")), "s3cret")
	plain := func(*http.Request) {}
	for i := range loginTries {
		if w := post(h, "nope", plain); w.Code != 401 {
			t.Fatalf("wrong token %d: got %d, want 401", i+1, w.Code)
		}
	}
	w := post(h, "s3cret", plain)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 {
		t.Errorf("right token while locked: got %d, Retry-After %q, cookies %v; want 429 and no cookie", w.Code, w.Header().Get("Retry-After"), w.Result().Cookies())
	}
}
