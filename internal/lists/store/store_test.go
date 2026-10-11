package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A file as the Node app wrote it (JSON.stringify(x, null, 1)): its key order,
// done after items, millisecond timestamps with trailing zeros, raw & and <.
const node = `{
 "id": "shop",
 "at": "2026-10-05T09:36:21.250Z",
 "text": "salt & <pepper>",
 "sevAt": "2026-10-05T09:36:21.250Z",
 "items": [
  {
   "id": "a1",
   "at": "2026-10-05T09:34:25.110Z",
   "text": "milk",
   "note": "oat",
   "sev": 3,
   "sevAt": "2026-10-05T09:34:25.000Z",
   "items": [],
   "done": "2026-10-05T10:37:57.600Z"
  }
 ]
}`

func TestPutKeepsNodeBytes(t *testing.T) {
	d := Dir(t.TempDir())
	p := d.path("shop")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(node), 0o644); err != nil {
		t.Fatal(err)
	}
	x, err := d.Get("shop")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Put(x); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != node {
		t.Errorf("a save without changes rewrote the file:\n%s", b)
	}
}
