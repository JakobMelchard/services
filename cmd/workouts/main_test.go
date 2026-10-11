package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JakobMelchard/services/internal/workouts/server"
)

func TestReadSheets(t *testing.T) {
	write := func(s string) string {
		p := filepath.Join(t.TempDir(), "sheets.json")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got, err := readSheets(write(`{"program":"p","active":"a","archive":"r"}`))
	if want := (server.Sheets{Program: "p", Active: "a", Archive: "r"}); err != nil || got != want {
		t.Errorf("got %+v, %v; want %+v", got, err, want)
	}
	for name, path := range map[string]string{
		"unset":   "",
		"missing": filepath.Join(t.TempDir(), "none.json"),
		"invalid": write("{"),
		"partial": write(`{"program":"p"}`),
	} {
		if _, err := readSheets(path); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
