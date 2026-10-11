// Package store keeps one JSON file per list under <dir>/lists, the whole tree
// inside, in the shape the Node app wrote. Writes go to a temp file in the same
// directory and are renamed over the old one, so a reader never sees a torn
// file. Syncthing conflict copies (*.sync-conflict-*) are never read.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JakobMelchard/services/internal/lists/model"
)

type Dir string

func (d Dir) path(id string) string { return filepath.Join(string(d), "lists", id+".json") }

// All is every well-formed list, by name. A corrupt, vanished or malformed
// file is skipped: every request reads all lists, so one bad file must not
// take the app down.
func (d Dir) All() ([]*model.Item, error) {
	es, err := os.ReadDir(filepath.Join(string(d), "lists"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var xs []*model.Item
	for _, e := range es {
		n := e.Name()
		if !strings.HasSuffix(n, ".json") || strings.Contains(n, ".sync-conflict-") {
			continue
		}
		if x, err := d.Get(strings.TrimSuffix(n, ".json")); err == nil {
			xs = append(xs, x)
		}
	}
	slices.SortFunc(xs, func(a, b *model.Item) int { return strings.Compare(a.Text, b.Text) })
	return xs, nil
}

// Get reads one list; fs.ErrNotExist when there is none, an error when the
// file is not a well-formed tree.
func (d Dir) Get(id string) (*model.Item, error) {
	b, err := os.ReadFile(d.path(id))
	if err != nil {
		return nil, err
	}
	var x model.Item
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	if !model.Valid(&x) {
		return nil, errors.New("malformed list " + id)
	}
	return &x, nil
}

// Put writes x atomically.
func (d Dir) Put(x *model.Item) error {
	// JSON.stringify(x, null, 1): one space indent, no HTML escaping, no final newline.
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", " ")
	if err := e.Encode(x); err != nil {
		return err
	}
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	p := d.path(x.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), "."+x.ID+".*.tmp")
	if err != nil {
		return err
	}
	// CreateTemp makes it 0600; the files are plain user data, readable like the rest.
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), p)
}

// Del removes a list; there is no trash.
func (d Dir) Del(id string) error {
	if err := os.Remove(d.path(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
