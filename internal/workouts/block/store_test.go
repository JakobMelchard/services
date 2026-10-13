package block_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JakobMelchard/services/internal/workouts/block"
	"github.com/JakobMelchard/services/internal/workouts/fakesheets"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

// fakeStore is a Store on a fakesheets.Sheets: program block w10-11 with one day,
// active block started from it, no week tab yet.
func fakeStore(t *testing.T) *block.Store {
	t.Helper()
	f := fakesheets.New()
	f.Add("program", "w10-11", [][]string{{"MON", ""}, {"Squat", "1 x 5"}})
	f.Add("active", "Block", [][]string{{"program", "w10-11"}, {"started", "2026-03-02"}})
	f.Add("archive", "Sheet1", nil)
	return storeOn(t, f)
}

// storeOn is a Store whose three sheets are the books of f.
func storeOn(t *testing.T, f *fakesheets.Sheets) *block.Store {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tok := fmt.Sprintf(`{"default":"a","accounts":{"a":{"access_token":"t","expires_at":%d}}}`, time.Now().Add(time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tok), 0o600); err != nil {
		t.Fatal(err)
	}
	client := func(id string) *gsheet.Client {
		c := gsheet.New(id, gsheet.WithConfigDir(dir))
		c.HTTP = &http.Client{Transport: fakesheets.Rewrite(strings.TrimPrefix(srv.URL, "http://"))}
		return c
	}
	return &block.Store{Program: client("program"), Active: client("active"), Archive: client("archive")}
}

// A block cell naming a program tab that does not exist warns with the tabs
// that do, so the mismatch can be fixed from the phone.
func TestWeekWarnsWithProgramTabs(t *testing.T) {
	f := fakesheets.New()
	f.Add("program", "w31-35", [][]string{{"MON", ""}, {"Squat", "1 x 5"}})
	f.Add("program", "w42-45", [][]string{{"MON", ""}, {"Squat", "1 x 5"}})
	f.Add("active", "Block", [][]string{{"program", "w41-46"}, {"started", "2026-10-05"}})
	f.Add("archive", "Sheet1", nil)
	v, err := storeOn(t, f).Week(t.Context(), "w41")
	if err != nil {
		t.Fatal(err)
	}
	want := "program tab w41-46: no such block in the program (it has w31-35, w42-45); days not started are missing"
	if v.Warning != want || len(v.Rows) != 0 {
		t.Fatalf("warning = %q, rows = %d; want %q, 0", v.Warning, len(v.Rows), want)
	}
}

// A client that goes away once the reads are done does not stop the writes
// halfway: the day it started is in the tab.
func TestEditFinishesWritesAfterCancel(t *testing.T) {
	st := fakeStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := st.Edit(ctx, "w10", "MON", func(v block.View) ([]block.Row, error) {
		cancel()
		return v.Rows, nil
	}); err != nil {
		t.Fatal(err)
	}
	v, err := st.Week(t.Context(), "w10")
	if err != nil || len(v.Rows) != 1 || v.Rows[0].Planned {
		t.Fatalf("w10 = %+v, %v; want Monday's set logged", v, err)
	}
	if _, err := st.Edit(ctx, "w10", "MON", func(v block.View) ([]block.Row, error) { return v.Rows, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("edit on a cancelled context: %v, want context.Canceled", err)
	}
}

// TestConcurrentEdits adds one exercise per goroutine to the same week, as
// two devices (or a retry) do; run with -race. Every one must be in the tab.
func TestConcurrentEdits(t *testing.T) {
	st := fakeStore(t)
	const n = 8
	errs := make(chan error, n)
	for i := range n {
		go func() {
			_, err := st.Edit(t.Context(), "w10", "MON", func(v block.View) ([]block.Row, error) {
				return block.AddExercise(v.Rows, v.Days, "MON", fmt.Sprintf("Curl %d", i), "1x10")
			})
			errs <- err
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	v, err := st.Week(t.Context(), "w10")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range v.Rows {
		got[r.Exercise] = true
	}
	if len(v.Rows) != n+1 || len(got) != n+1 {
		t.Fatalf("w10 has %d rows of %d exercises after %d concurrent adds to 1 set, want %d: %v", len(v.Rows), len(got), n, n+1, got)
	}
}

// Two blocks that log the same calendar week both keep their archive.
func TestNextKeepsEarlierArchive(t *testing.T) {
	st := fakeStore(t)
	for range 2 {
		if _, err := st.Edit(t.Context(), "w10", "MON", func(v block.View) ([]block.Row, error) { return v.Rows, nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Next(t.Context(), "w10-11"); err != nil {
			t.Fatal(err)
		}
	}
	tabs, err := st.Archive.Tabs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range tabs {
		got = append(got, x.Title)
	}
	if want := []string{"2026 w10", "2026 w10 (2)"}; !slices.Equal(got, want) {
		t.Fatalf("archive tabs = %q, want %q", got, want)
	}
}
