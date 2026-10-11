package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/server"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
	"github.com/getsentry/sentry-go"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	sheetsFile := flag.String("sheets", "", `JSON file with the spreadsheet IDs (required): {"program": one tab per training day, "active": week tabs W1..Wn, "archive": finished blocks' weeks}`)
	embed := flag.String("embed", "", "optional published embed URL (.../pubhtml) shown in the Sheet view")
	account := flag.String("account", "", "gsheet account email (default account if empty)")
	saFile := flag.String("service-account", "", "service-account key JSON file; env GSHEET_SERVICE_PRINCIPAL_JSON wins")
	token := flag.String("token", "", "bearer token for non-GET routes (env WORKOUTS_TOKEN); required unless -addr is loopback")
	tokenFile := flag.String("token-file", "", "read the token from this file (env WORKOUTS_TOKEN_FILE)")
	flag.Parse()
	if *token == "" {
		*token = os.Getenv("WORKOUTS_TOKEN")
	}
	if *tokenFile == "" {
		*tokenFile = os.Getenv("WORKOUTS_TOKEN_FILE")
	}
	if *token == "" && *tokenFile != "" {
		b, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatalf("token file: %v", err)
		}
		*token = strings.TrimSpace(string(b))
	}

	sheets, err := readSheets(*sheetsFile)
	if err != nil {
		log.Fatal(err)
	}
	// Empty SENTRY_DSN disables the client (noop transport), nothing is sent.
	// Environment comes from env SENTRY_ENVIRONMENT, default production.
	env := os.Getenv("SENTRY_ENVIRONMENT")
	if env == "" {
		env = "production"
	}
	if err := sentry.Init(sentry.ClientOptions{Dsn: os.Getenv("SENTRY_DSN"), Environment: env}); err != nil {
		log.Fatalf("sentry: %v", err)
	}
	var opts []gsheet.Option
	if *account != "" {
		opts = append(opts, gsheet.WithAccount(*account))
	}
	if *saFile != "" {
		b, err := os.ReadFile(*saFile)
		if err != nil {
			log.Fatalf("service account: %v", err)
		}
		opts = append(opts, gsheet.WithServiceAccount(b))
	}
	err = server.Start(*embed, sheets, *addr, *token, opts...)
	sentry.Flush(2 * time.Second)
	if err != nil {
		log.Fatal(err)
	}
}

// readSheets keeps the spreadsheet IDs out of the repo and the service definition.
func readSheets(path string) (server.Sheets, error) {
	var s server.Sheets
	if path == "" {
		return s, errors.New("-sheets is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if s.Program == "" || s.Active == "" || s.Archive == "" {
		return s, fmt.Errorf("%s: program, active and archive are required", path)
	}
	return s, nil
}
