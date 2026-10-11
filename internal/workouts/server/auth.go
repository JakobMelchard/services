package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cookieName = "workouts_auth"

// checkAddr refuses to serve without a token on anything but loopback.
func checkAddr(addr, token string) error {
	if token != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad -addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q without a token: set -token or WORKOUTS_TOKEN, or bind loopback (-addr 127.0.0.1:8080)", addr)
}

// cookieValue is what the login cookie holds, so the raw token never sits in the browser.
func cookieValue(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func eq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// requireToken gates every non-GET/HEAD request, except POST /login, on
// "Authorization: Bearer <token>" or the login cookie. Empty token disables it.
func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	cv := cookieValue(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead ||
			(r.Method == http.MethodPost && r.URL.Path == "/login") {
			next.ServeHTTP(w, r)
			return
		}
		if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && eq(b, token) {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(cookieName); err == nil && eq(c.Value, cv) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// Wrong tokens allowed in a row before POST /login is refused for loginLock.
// The count is global: behind a reverse proxy every client has one address.
const (
	loginTries = 5
	loginLock  = time.Minute
)

// login checks the posted token and sets an HttpOnly, SameSite=Strict cookie,
// Secure when the request came over TLS (directly, or per X-Forwarded-Proto
// from the proxy in front), so plain http on loopback keeps working.
func login(tpl *template.Template, token string) http.HandlerFunc {
	var (
		mu     sync.Mutex
		fails  int
		locked time.Time
	)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if err := tpl.ExecuteTemplate(w, "login.html", nil); err != nil {
				log.Printf("login: %v", err)
			}
			return
		}
		got := r.PostFormValue("token") // reads the body: before the lock
		mu.Lock()
		defer mu.Unlock()
		if wait := time.Until(locked); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, "too many wrong tokens; try again later", http.StatusTooManyRequests)
			return
		}
		if !eq(got, token) {
			if fails++; fails >= loginTries {
				fails, locked = 0, time.Now().Add(loginLock)
			}
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		fails = 0
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    cookieValue(token),
			Path:     "/",
			MaxAge:   30 * 24 * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
