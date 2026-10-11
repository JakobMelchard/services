package model

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func days(d float64) time.Time { return now.Add(-time.Duration(d * 24 * float64(time.Hour))) }

func it(id string, kids ...*Item) *Item { return &Item{ID: id, Text: id, At: now, Items: kids} }

func ids(x *Item) []string {
	out := []string{x.ID}
	for _, c := range x.Items {
		out = append(out, ids(c)...)
	}
	return out
}

func TestSevEscalatesFromSevAt(t *testing.T) {
	for d, want := range map[float64]int{0: 1, 29: 1, 30: 2, 36: 2, 37: 3} {
		if got := Sev(&Item{At: days(d)}, now); got != want {
			t.Errorf("sev 1 at %v days = %d, want %d", d, got, want)
		}
	}
	for d, want := range map[float64]int{0: 2, 6: 2, 7: 3} {
		if got := Sev(&Item{Sev: 2, At: days(d)}, now); got != want {
			t.Errorf("sev 2 at %v days = %d, want %d", d, got, want)
		}
	}
	if got := Sev(&Item{Sev: 3, At: now}, now); got != 3 {
		t.Errorf("sev 3 = %d", got)
	}
	// Setting urgency restarts the clock, or an old item ignores the stored level.
	if got := Sev(&Item{Sev: 2, At: days(200), SevAt: now}, now); got != 2 {
		t.Errorf("sev 2 set today on an old item = %d, want 2", got)
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)), NewID(time.Date(2030, 6, 15, 0, 0, 0, 0, time.UTC))
	if a >= b {
		t.Errorf("%q should sort before %q", a, b)
	}
	if x, y := NewID(now), NewID(now); x == y || !ID.MatchString(x) {
		t.Errorf("same instant: %q %q", x, y)
	}
}

func TestChainCloneSorted(t *testing.T) {
	b := it("b")
	b.Done = now
	c := it("c")
	c.Sev = 3
	root := it("r", it("a", b), c)
	var got []string
	for _, x := range Chain(root, "b") {
		got = append(got, x.ID)
	}
	if !slices.Equal(got, []string{"r", "a", "b"}) || Chain(root, "zz") != nil {
		t.Errorf("chain = %q", got)
	}
	y := Clone(root, now)
	seen := map[string]bool{}
	for _, id := range append(ids(root), ids(y)...) {
		seen[id] = true
	}
	if len(seen) != 8 {
		t.Errorf("clone shares ids: %v", seen)
	}
	if !y.Items[0].Items[0].Done.IsZero() || !y.At.Equal(root.At) {
		t.Errorf("clone must reset done and keep at: %+v", y.Items[0].Items[0])
	}
	// open first, most urgent first; done at the bottom even when urgent
	d, l, h := it("d"), it("l"), it("h")
	d.Done, d.Sev, h.Sev = now, 3, 3
	var order []string
	for _, x := range Sorted([]*Item{d, l, h}, now) {
		order = append(order, x.ID)
	}
	if !slices.Equal(order, []string{"h", "l", "d"}) {
		t.Errorf("sorted = %q", order)
	}
	// a duplicate spliced in after its source stays directly below it once sorted
	a1, b1 := it("a"), it("b")
	a1.At, b1.At = days(60), days(30)
	a2 := Clone(a1, now)
	a2.ID = "a2"
	order = order[:0]
	for _, x := range Sorted([]*Item{a1, a2, b1}, now) {
		order = append(order, x.ID)
	}
	if !slices.Equal(order, []string{"a", "a2", "b"}) {
		t.Errorf("sorted with copy = %q", order)
	}
}

func TestFilter(t *testing.T) {
	buns, milk := it("c"), it("d")
	buns.Text, buns.Done = "buns", now
	milk.Text, milk.Sev = "milk", 3
	rye, bakery := it("b"), it("a")
	rye.Text, bakery.Text = "rye bread", "bakery"
	bakery.Items = []*Item{rye, buns}
	root := it("r", bakery, milk)
	root.Text = "shop"
	f := func(q string) []string {
		if r := Filter(root, q, now); r != nil {
			return ids(r)
		}
		return nil
	}
	for q, want := range map[string][]string{
		"":          {"r", "a", "b", "c", "d"},
		"rye":       {"r", "a", "b"}, // ancestors kept for a matching child
		"RYE":       {"r", "a", "b"}, // case-insensitive
		"bakery":    {"r", "a"},      // matches itself, not its children
		"-done":     {"r", "a", "b", "d"},
		"sev:3":     {"r", "d"},
		"shop nope": nil,
		"bak":       nil, // exact tokens, no prefixes
	} {
		if got := f(q); !slices.Equal(got, want) {
			t.Errorf("filter %q = %q, want %q", q, got, want)
		}
	}
}

func TestValidAndJSON(t *testing.T) {
	for _, s := range []string{"{", "", "null", "42", "{}", `{"id":"x","text":"x","at":"2026-01-01T00:00:00Z","items":[null]}`, `{"id":"x","text":"x","at":"2026-01-01T00:00:00Z","items":[{"id":"c","text":"c","items":[]}]}`, `{"id":"../x","text":"x","at":"2026-01-01T00:00:00Z","items":[]}`} {
		var x Item
		if json.Unmarshal([]byte(s), &x) == nil && Valid(&x) {
			t.Errorf("%q should not be valid", s)
		}
	}
	var x Item
	if err := json.Unmarshal([]byte(`{"id":"x","text":"x","at":"2026-01-01T00:00:00.000Z"}`), &x); err != nil || !Valid(&x) {
		t.Fatalf("missing items should read as none: %v", err)
	}
	b, _ := json.Marshal(&x)
	if s := string(b); !strings.Contains(s, `"items":[]`) || strings.Contains(s, "sevAt") || strings.Contains(s, "done") || strings.Contains(s, "sev\"") {
		t.Errorf("absent fields must stay unstored: %s", s)
	}
}
