//go:build demo

// Command demo serves the UI against the in-memory Sheets fake
// (internal/workouts/fakesheets) seeded with fixed demo data, for README screenshots
// (scripts/workouts/screenshots.sh). Edits are kept in memory until it exits. Dev only:
// the demo build tag keeps it out of normal builds, and every gsheet request
// is rewritten to the local fake server, so it never talks to Google.
//
//	go run -tags demo ./cmd/demo [-addr 127.0.0.1:8561]
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/JakobMelchard/services/internal/workouts/fakesheets"
	"github.com/JakobMelchard/services/internal/workouts/gsheet"
	"github.com/JakobMelchard/services/internal/workouts/server"
)

// seed fills f with the demo block: two program tabs, a started block, week
// w40 with three days logged and an empty archive.
func seed(f *fakesheets.Sheets) {
	f.Add("program", "w38-43", [][]string{
		{"MONDAY (SQUAT)", ""},
		{"Squat", "3 x 5|5|3|3 / 110kg @8 3m"},
		{"Romanian Deadlift", "3 x 8 70kg @7 2m"},
		{"Leg Curl", "3 x 10-12 40kg"},
		{"", ""},
		{"TUESDAY (CLIMB)", ""},
		{"Hangboard", "5 x 7''"},
		{"Bouldering", "60-90'"},
		{"Weighted Chin-Up", "3 x 6|6|5|5 / 15kg"},
		{"", ""},
		{"WEDNESDAY (REST)", ""},
		{"", ""},
		{"THURSDAY (BENCH)", ""},
		{"Bench Press", "4 x 6|6|4|4 / 77.5kg @8 3m"},
		{"Barbell Row", "3 x 8 60kg 2m"},
		{"Lateral Raise", "3 x 12-15 10kg"},
		{"", ""},
		{"SATURDAY (DEADLIFT)", ""},
		{"Deadlift", "3 x 5|5|3|3 / 140kg @8 3m"},
		{"OHP", "3 x 8|6|6|4 / 40kg"},
		{"Face Pull", "3 x 15 20kg"},
		{"", ""},
		{"SUNDAY (RUN)", ""},
		{"Easy Run", "40-50'"},
	})
	f.Add("program", "w44-49", [][]string{{"MONDAY", ""}, {"Squat", "5 x 5 90kg"}})
	f.Add("active", "Block", [][]string{{"program", "w38-43"}, {"started", "2026-09-14"}})
	w40 := [][]string{
		{"MONDAY (SQUAT)", ""},
		{"Squat", "1x3 110kg @8 3m"},
		{"Squat", "1x3 110kg @8 3m"},
		{"Squat", "1x3 110kg @8.5 3m"},
		{"Romanian Deadlift", "1x8 70kg @7 2m"},
		{"Romanian Deadlift", "1x8 70kg @7 2m"},
		{"Romanian Deadlift", "1x8 72.5kg @7.5 2m"},
		{"Leg Curl", "1x12 40kg"},
		{"Leg Curl", "1x11 40kg"},
		{"Leg Curl", "1x10 40kg"},
		{"", ""},
		{"TUESDAY (CLIMB)", ""},
		{"Hangboard", "1x7s"},
		{"Hangboard", "1x7s"},
		{"Hangboard", "1x7s"},
		{"Hangboard", "1x7s"},
		{"Hangboard", "1x6s"},
		{"Bouldering", "1x75m"},
		{"Weighted Chin-Up", "1x5 15kg"},
		{"Weighted Chin-Up", "1x5 15kg"},
		{"Weighted Chin-Up", "1x4 15kg"},
		{"", ""},
		{"THURSDAY (BENCH)", ""},
		{"Bench Press", "1x4 77.5kg @7.5 3m"},
		{"Bench Press", "1x4 77.5kg @8 3m"},
		{"Bench Press", "1x4 77.5kg @8 3m"},
		{"Bench Press", "1x4 77.5kg @8.5 3m"},
		{"Barbell Row", "1x8 60kg 2m"},
		{"Barbell Row", "1x8 60kg 2m"},
		{"Barbell Row", "1x8 60kg 2m"},
		{"Lateral Raise", "1x12-15 10kg"},
		{"Lateral Raise", "1x12-15 10kg"},
		{"Lateral Raise", "1x12-15 10kg"},
	}
	notes := make([]string, len(w40))
	notes[2], notes[17], notes[26] = "felt #fast, depth good", "6b project, two sends #pr", "last rep slow #grind"
	f.Add("active", "w40", w40, notes...)
}

func main() {
	f := fakesheets.New()
	seed(f)
	fake := httptest.NewServer(f)
	addr := flag.String("addr", "127.0.0.1:8561", "listen address (loopback)")
	flag.Parse()
	host := strings.TrimPrefix(fake.URL, "http://")
	dir, err := os.MkdirTemp("", "workouts-demo")
	if err != nil {
		log.Fatal(err)
	}
	os.Unsetenv("GSHEET_SERVICE_PRINCIPAL_JSON")
	os.Setenv("GSHEET_TOKENS_JSON", `{"accounts":{"demo@example.com":{"access_token":"demo","expires_at":99999999999}},"default":"demo@example.com"}`)
	opt := func(c *gsheet.Client) { c.HTTP = &http.Client{Transport: fakesheets.Rewrite(host)} }
	err = server.Start("", server.Sheets{Program: "program", Active: "active", Archive: "archive"}, *addr, "", gsheet.WithConfigDir(dir), opt)
	os.RemoveAll(dir)
	if err != nil {
		log.Fatal(err)
	}
}
