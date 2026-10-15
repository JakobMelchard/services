package handler

import (
	"strings"
	"testing"
)

// TestStaticRoutes covers the PWA shell: the page links the manifest and
// registers the worker, both are served from /, the icons and assets from
// /static. Content types matter for install and for the stylesheet.
func TestStaticRoutes(t *testing.T) {
	f := mk(t)
	for path, want := range map[string][2]string{
		"/":                            {"text/html; charset=utf-8", `<script type="module" src="/static/app.js">`},
		"/static/app.js":               {"text/javascript; charset=utf-8", `register({ version: "1" })`},
		"/sw.js":                       {"text/javascript", `importScripts("/web/sw.js")`},
		"/static/manifest.webmanifest": {"application/manifest+json", `"id": "/"`},
		"/web/tokens.css":              {"text/css; charset=utf-8", "--bg"},
		"/web/base.css":                {"text/css; charset=utf-8", ":focus-visible"},
		"/web/sw.js":                   {"text/javascript; charset=utf-8", "WEB_SW"},
		"/web/register.js":             {"text/javascript; charset=utf-8", "register"},
		"/static/offline.html":         {"text/html; charset=utf-8", "offline"},
		"/static/style.css":            {"text/css; charset=utf-8", "safe-area-inset"},
		"/static/htmx.min.js":          {"text/javascript; charset=utf-8", "htmx"},
		"/static/icon-192.png":         {"image/png", "\x89PNG"},
		"/static/icon-512.png":         {"image/png", "\x89PNG"},
		"/static/icon-maskable.png":    {"image/png", "\x89PNG"},
	} {
		w := f.req("GET", path)
		if w.Code != 200 || w.Header().Get("Content-Type") != want[0] || !strings.Contains(w.Body.String(), want[1]) {
			t.Errorf("GET %s: %d %s, want 200 %s containing %q", path, w.Code, w.Header().Get("Content-Type"), want[0], want[1])
		}
	}
	for _, path := range []string{"/static/", "/static/nope.css", "/nope"} {
		if w := f.req("GET", path); w.Code != 404 {
			t.Errorf("GET %s: %d, want 404", path, w.Code)
		}
	}
	// the page is dark only on the vendored tokens: no light scheme in the stylesheet
	if css := f.text("GET", "/static/style.css"); strings.Contains(css, "prefers-color-scheme") || strings.Contains(css, "color-scheme:light") {
		t.Error("style.css carries a colour scheme")
	}
}
