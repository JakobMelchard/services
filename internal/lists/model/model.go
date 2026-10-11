// Package model is the list tree: a list is an item, its entries are items,
// entries nest. Everything recursive goes through the helpers here.
package model

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Item is a list or an entry. Sev is the stored base urgency, 2 or 3; 0 means
// 1 and stays unstored. SevAt is when urgency was last set, zero means At.
type Item struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	Note  string    `json:"note,omitempty"`
	Sev   int       `json:"sev,omitempty"`
	At    time.Time `json:"at"`
	SevAt time.Time `json:"sevAt,omitzero"`
	Done  time.Time `json:"done,omitzero"`
	Items []*Item   `json:"items"`
}

// MarshalJSON writes what the Node app wrote: its key order, millisecond
// timestamps (toISOString) and no HTML escaping, so saving a list that did not
// change leaves its file byte-identical.
func (x *Item) MarshalJSON() ([]byte, error) {
	ms := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	err := e.Encode(struct {
		ID    string  `json:"id"`
		At    string  `json:"at"`
		Text  string  `json:"text"`
		Note  string  `json:"note,omitempty"`
		Sev   int     `json:"sev,omitempty"`
		SevAt string  `json:"sevAt,omitempty"`
		Items []*Item `json:"items"`
		Done  string  `json:"done,omitempty"`
	}{x.ID, ms(x.At), x.Text, x.Note, x.Sev, ms(x.SevAt), x.Items, ms(x.Done)})
	return b.Bytes(), err
}

// ID is the shape of every id: NewID and the fixtures produce it, and ids
// become file names, so nothing else gets through the routes.
var ID = regexp.MustCompile(`^[a-z0-9-]+$`)

const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

// NewID is time-sortable and opaque, never derived from user input.
func NewID(now time.Time) string {
	b := []byte{0, 0, 0, 0}
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return strconv.FormatInt(now.UnixMilli(), 36) + "-" + string(b)
}

// Valid is true when x has the shape the helpers and views rely on, at every
// depth. A nil Items is made empty so it marshals as [].
func Valid(x *Item) bool {
	if x == nil || !ID.MatchString(x.ID) || x.At.IsZero() {
		return false
	}
	if x.Items == nil {
		x.Items = []*Item{}
	}
	for _, c := range x.Items {
		if !Valid(c) {
			return false
		}
	}
	return true
}

// Chain is the path root..item to the item with id, or nil. The last element
// is the item, the one before it its parent.
func Chain(root *Item, id string) []*Item {
	if root.ID == id {
		return []*Item{root}
	}
	for _, c := range root.Items {
		if r := Chain(c, id); r != nil {
			return append([]*Item{root}, r...)
		}
	}
	return nil
}

// up is the days at each level until the next one.
var up = [...]float64{0, 30, 7}

// Sev is the effective urgency, 3 the most urgent: 1 → 2 after 30 days, 2 → 3
// after 7 more, counted from SevAt (falling back to At). Ageing from when
// urgency was last set, not from creation, keeps the stored level meaningful
// on old items.
func Sev(x *Item, now time.Time) int {
	s, at := max(x.Sev, 1), x.SevAt
	if at.IsZero() {
		at = x.At
	}
	d := now.Sub(at).Hours() / 24
	for s < 3 && d >= up[s] {
		d -= up[s]
		s++
	}
	return s
}

// Clone is a deep copy with fresh ids and nothing done. At and SevAt are kept
// so the copy sorts identically to its source: Sorted is stable and the copy
// is spliced in right after it, which puts a duplicate directly below.
func Clone(x *Item, now time.Time) *Item {
	y := *x
	y.ID, y.Done, y.Items = NewID(now), time.Time{}, make([]*Item, len(x.Items))
	for i, c := range x.Items {
		y.Items[i] = Clone(c, now)
	}
	return &y
}

// Sorted is open first, most urgent first, then oldest first; done at the bottom.
func Sorted(xs []*Item, now time.Time) []*Item {
	ys := slices.Clone(xs)
	slices.SortStableFunc(ys, func(a, b *Item) int {
		if da, db := !a.Done.IsZero(), !b.Done.IsZero(); da != db {
			if da {
				return 1
			}
			return -1
		}
		if d := Sev(b, now) - Sev(a, now); d != 0 {
			return d
		}
		return a.At.Compare(b.At)
	})
	return ys
}

// Filter prunes x to the items matching q: the item's own Text and Note
// words plus the synthetic sev:N and done/open. A parent survives when any
// child does; a match does not pull its children in. Nil when nothing matches.
func Filter(x *Item, q string, now time.Time) *Item {
	kids := []*Item{}
	for _, c := range x.Items {
		if k := Filter(c, q, now); k != nil {
			kids = append(kids, k)
		}
	}
	// Lowercased throughout so Sourdough and sourdough both hit.
	tags := strings.Fields(strings.ToLower(x.Text + " " + x.Note))
	tags = append(tags, "sev:"+strconv.Itoa(Sev(x, now)), "open")
	if !x.Done.IsZero() {
		tags[len(tags)-1] = "done"
	}
	if !match(tags, strings.ToLower(q)) && len(kids) == 0 {
		return nil
	}
	y := *x
	y.Items = kids
	return &y
}

// match: `a b -c k:v` means every positive term present, no negative term
// present. Terms are exact, so a prefix does not hit. Empty matches all.
func match(tags []string, q string) bool {
	for _, t := range strings.Fields(q) {
		if neg := strings.HasPrefix(t, "-"); slices.Contains(tags, strings.TrimPrefix(t, "-")) == neg {
			return false
		}
	}
	return true
}
