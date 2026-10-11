package server

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
	"github.com/JakobMelchard/services/internal/workouts/handler"
	"github.com/JakobMelchard/services/internal/workouts/ui"
	"github.com/JakobMelchard/web"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

// Sheets are the spreadsheet IDs of a block (see package block).
type Sheets struct{ Program, Active, Archive string }

// Start serves the app on addr until SIGINT or SIGTERM.
func Start(embed string, sheets Sheets, addr, token string, opts ...gsheet.Option) error {
	if err := checkAddr(addr, token); err != nil {
		return err
	}
	app, err := routes(embed, sheets, token, opts...)
	if err != nil {
		return err
	}
	// SIGINT/SIGTERM cancels every request context: handlers reading from
	// Sheets (or waiting out a quota backoff) return at once, a write already
	// under way (block.Store) gets the 5s below to finish.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              addr,
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		grace, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(grace); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("listening on %s (program %s, active %s, archive %s)", addr, sheets.Program, sheets.Active, sheets.Archive)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// ListenAndServe returns as soon as Shutdown starts; wait for it to end.
	<-done
	return nil
}

// routes is the app: every route, behind the token check and the Sentry
// panic reporter.
func routes(embed string, sheets Sheets, token string, opts ...gsheet.Option) (http.Handler, error) {
	tpl, err := template.ParseFS(ui.Templates, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := handler.New(gsheet.New(sheets.Program, opts...), gsheet.New(sheets.Active, opts...), gsheet.New(sheets.Archive, opts...), tpl, embed)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.Index)
	mux.HandleFunc("GET /block", h.Block)
	mux.HandleFunc("POST /block", h.NextBlock)
	mux.HandleFunc("GET /programs/{tab}", h.Program)
	mux.HandleFunc("GET /weeks/{tab}/rows", h.Rows)
	mux.HandleFunc("PATCH /weeks/{tab}/rows", h.EditRow)
	mux.HandleFunc("POST /weeks/{tab}/rows/duplicate", h.DuplicateRow)
	mux.HandleFunc("POST /weeks/{tab}/rows/delete", h.DeleteRow)
	mux.HandleFunc("POST /weeks/{tab}/days/{day}/start", h.StartDay)
	mux.HandleFunc("POST /weeks/{tab}/days/{day}/exercises", h.AddExercise)
	if token != "" {
		mux.HandleFunc("GET /login", login(tpl, token))
		mux.HandleFunc("POST /login", login(tpl, token))
	}

	staticFS, err := fs.Sub(ui.Static, "static")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	dist, err := fs.Sub(web.FS, "dist")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /web/", http.StripPrefix("/web/", http.FileServer(http.FS(dist))))
	// The service worker must be served from / to control the whole app.
	mux.HandleFunc("GET /sw.js", rootFile(staticFS, "sw.js", "text/javascript"))
	// Under /static so the manifest's relative icon paths resolve to the icons.
	manifest := web.Manifest(map[string]string{"name": "workouts", "short_name": "workouts", "description": "Workout logging backed by Google Sheets", "start_url": "/", "scope": "/"})
	mux.HandleFunc("GET /static/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(manifest)
	})

	// Panics are reported to Sentry (if SENTRY_DSN is set) and repanicked, so
	// net/http still recovers and logs them as before.
	report := sentryhttp.New(sentryhttp.Options{Repanic: true})
	return report.Handle(requireToken(token, mux)), nil
}

func rootFile(fsys fs.FS, name, ctype string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := w.Write(b); err != nil {
			log.Printf("%s: %v", name, err)
		}
	}
}
