// Command gsheet syncs Google Sheets tabs with TSV: pull/push ranges, open
// a spreadsheet's tabs in nvim, manage tabs, and OAuth accounts (PKCE
// loopback login) in ~/.config/gsheet.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/JakobMelchard/services/internal/workouts/gsheet"
)

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gsheet:", err)
		os.Exit(1)
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"open"}
	}
	cmd, rest := args[0], args[1:]
	if cmd == "auth" {
		authCmd(rest)
		return
	}
	fs := flag.NewFlagSet("gsheet "+cmd, flag.ExitOnError)
	account := fs.String("account", "", "account email (default account if empty)")
	header := ""
	if cmd == "addtab" {
		fs.StringVar(&header, "header", "", "comma-separated header row, written bold")
	}
	pos := parse(fs, rest)
	switch cmd {
	case "pull", "meta", "addtab", "rmtab", "checkbox", "list":
		sheetCmd(cmd, *account, header, pos)
	case "push":
		pushCmd(*account, pos)
	case "open":
		openCmd(*account)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(2)
	}
}

// parse parses fs over args, flags before, between or after the positional
// args, and returns the positional ones.
func parse(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		die(fs.Parse(args))
		if fs.NArg() == 0 {
			return pos
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func authCmd(rest []string) {
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gsheet auth <setup|login|list|revoke|default> ...")
		os.Exit(2)
	}
	c := gsheet.New("") // auth uses no sheet id
	sub := rest[0]
	args := rest[1:]
	switch sub {
	case "setup":
		if len(args) != 2 {
			die(fmt.Errorf("usage: gsheet auth setup <client_id> <client_secret>"))
		}
		die(c.SetupClient(args[0], args[1]))
	case "login":
		fs := flag.NewFlagSet("gsheet auth login", flag.ExitOnError)
		noBrowser := fs.Bool("no-browser", false, "print the URL only, do not open a browser")
		die(fs.Parse(args))
		email, err := c.Login(os.Stdout, !*noBrowser)
		die(err)
		fmt.Println("logged in as", email)
	case "list":
		emails, def, err := c.Accounts()
		if err != nil {
			die(err)
		}
		for _, e := range emails {
			marker := " "
			if e == def {
				marker = "*"
			}
			fmt.Printf("%s %s\n", marker, e)
		}
	case "revoke":
		if len(args) != 1 {
			die(fmt.Errorf("usage: gsheet auth revoke <email>"))
		}
		die(c.Revoke(args[0]))
	case "default":
		if len(args) != 1 {
			die(fmt.Errorf("usage: gsheet auth default <email>"))
		}
		die(c.SetDefault(args[0]))
	default:
		fmt.Fprintf(os.Stderr, "unknown auth command: %s\n", sub)
		os.Exit(2)
	}
}

func newClient(sheetID, account string) *gsheet.Client {
	return gsheet.New(sheetID, gsheet.WithAccount(account))
}

func sheetCmd(cmd, account, header string, rest []string) {
	ctx := context.Background()
	switch cmd {
	case "pull":
		if len(rest) < 2 {
			die(fmt.Errorf("usage: gsheet pull [--account A] <sheet> <range>"))
		}
		c := newClient(rest[0], account)
		grid, err := c.ReadRange(ctx, rest[1])
		die(err)
		fmt.Print(tsv(grid))
	case "meta":
		if len(rest) < 1 {
			die(fmt.Errorf("usage: gsheet meta [--account A] <sheet>"))
		}
		c := newClient(rest[0], account)
		tabs, err := c.Tabs(ctx)
		die(err)
		for _, t := range tabs {
			fmt.Printf("%s\t%d\t%dx%d\n", t.Title, t.SheetID, t.Rows, t.Cols)
		}
	case "list":
		c := newClient("", account)
		sps, err := c.List(ctx)
		die(err)
		for _, s := range sps {
			fmt.Printf("%s\t%s\n", s.Name, s.ID)
		}
	case "addtab":
		if len(rest) < 2 {
			die(fmt.Errorf("usage: gsheet addtab [--account A] <sheet> <name> [--header a,b,c]"))
		}
		c := newClient(rest[0], account)
		gid, err := c.AddTab(ctx, rest[1], header)
		die(err)
		fmt.Println(gid)
	case "rmtab":
		if len(rest) < 2 {
			die(fmt.Errorf("usage: gsheet rmtab [--account A] <sheet> <name>"))
		}
		c := newClient(rest[0], account)
		die(c.DelTab(ctx, rest[1]))
	case "checkbox":
		if len(rest) < 5 {
			die(fmt.Errorf("usage: gsheet checkbox [--account A] <sheet> <tab> <col> <start> <end>"))
		}
		c := newClient(rest[0], account)
		start, err1 := strconv.Atoi(rest[3])
		end, err2 := strconv.Atoi(rest[4])
		if err1 != nil || err2 != nil {
			die(fmt.Errorf("bad start/end rows"))
		}
		die(c.Checkbox(ctx, rest[1], rest[2], start, end))
	}
}

func pushCmd(account string, rest []string) {
	if len(rest) < 2 {
		die(fmt.Errorf("usage: gsheet push [--account A] <sheet> <range>"))
	}
	ctx := context.Background()
	c, rng := newClient(rest[0], account), rest[1]
	data, err := io.ReadAll(os.Stdin)
	die(err)
	var grid [][]string
	if tsv := strings.TrimSuffix(string(data), "\n"); tsv != "" {
		for l := range strings.SplitSeq(tsv, "\n") {
			grid = append(grid, strings.Split(l, "\t"))
		}
	}
	// Open-ended ranges ("Tab!A1:Z") get row inserts/deletes first, so rows
	// keep their formatting and notes. Bounded ones skip that: deleting rows
	// would pull cells below the range into it.
	if tab, start, open := splitRange(rng); open {
		old, err := c.ReadRange(ctx, rng)
		die(err)
		die(c.EditRows(ctx, tab, start, gsheet.DiffRows(old, grid)))
	}
	// Clear first: a grid shorter or narrower than the range's current
	// values would otherwise leave the old trailing cells in place.
	die(c.Clear(ctx, rng))
	if len(grid) > 0 {
		die(c.Write(ctx, rng, grid))
	}
	fmt.Println("ok")
}

var a1Re = regexp.MustCompile(`^[A-Z]+(\d*)(:[A-Z]+(\d*))?$`)

// splitRange parses "'Tab'!A5:C" into the tab title, the 0-based start row
// and whether the range runs to the last row. A bare tab name is all rows.
func splitRange(rng string) (tab string, start int, open bool) {
	i := strings.LastIndex(rng, "!")
	if i < 0 {
		return rng, 0, true
	}
	tab = rng[:i]
	if len(tab) > 1 && tab[0] == '\'' && tab[len(tab)-1] == '\'' {
		tab = strings.ReplaceAll(tab[1:len(tab)-1], "''", "'")
	}
	m := a1Re.FindStringSubmatch(rng[i+1:])
	if m == nil {
		return tab, 0, false
	}
	if m[1] != "" {
		n, _ := strconv.Atoi(m[1])
		start = n - 1
	}
	return tab, start, m[2] != "" && m[3] == ""
}

// tsv renders grid as TSV lines padded to the widest row.
func tsv(grid [][]string) string {
	width := 0
	for _, r := range grid {
		width = max(width, len(r))
	}
	var b strings.Builder
	for _, r := range grid {
		row := make([]string, width)
		copy(row, r)
		b.WriteString(strings.Join(row, "\t") + "\n")
	}
	return b.String()
}

// link is one links.json entry: the nvim plugin binds a TSV file by it.
type link struct {
	Account string `json:"account,omitempty"`
	Sheet   string `json:"sheet"`
	Range   string `json:"range"`
	Title   string `json:"title"`
}

// openCmd picks a spreadsheet with fzf, saves each tab as TSV under
// $GSHEET_DIR (default ~/.local/share/gsheet), records the files in
// links.json for the nvim plugin, and opens them in nvim.
func openCmd(account string) {
	ctx := context.Background()
	sps, err := newClient("", account).List(ctx)
	die(err)
	lines := make([]string, len(sps))
	for i, s := range sps {
		lines[i] = s.Name + "\t" + s.ID
	}
	name, id, _ := strings.Cut(pick(lines, "spreadsheet"), "\t")
	c := newClient(id, account)
	tabs, err := c.Tabs(ctx)
	die(err)
	dir := os.Getenv("GSHEET_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		die(err)
		dir = filepath.Join(home, ".local", "share", "gsheet")
	}
	linksFile := filepath.Join(dir, "links.json")
	links := map[string]link{}
	if b, err := os.ReadFile(linksFile); err == nil {
		die(json.Unmarshal(b, &links))
	}
	var paths []string
	for _, t := range tabs {
		rng := gsheet.Columns(t.Title, t.Cols)
		grid, err := c.ReadRange(ctx, rng)
		die(err)
		p := filepath.Join(dir, safeName(name), safeName(t.Title)+".tsv")
		die(os.MkdirAll(filepath.Dir(p), 0o755))
		die(os.WriteFile(p, []byte(tsv(grid)), 0o644))
		links[p] = link{Account: account, Sheet: id, Range: rng, Title: t.Title}
		paths = append(paths, p)
	}
	b, err := json.MarshalIndent(links, "", "  ")
	die(err)
	die(os.WriteFile(linksFile, b, 0o644))
	fmt.Printf("opened %d tabs from %s\n", len(paths), name)
	if sock := os.Getenv("NVIM"); sock != "" {
		for _, p := range paths {
			die(exec.Command("nvim", "--server", sock, "--remote-tab-silent", p).Run())
		}
		return
	}
	nvim, err := exec.LookPath("nvim")
	die(err)
	die(syscall.Exec(nvim, append([]string{"nvim", "-p"}, paths...), os.Environ()))
}

// pick runs fzf over lines and returns the chosen one; cancel exits 1.
func pick(lines []string, prompt string) string {
	if len(lines) == 0 {
		die(fmt.Errorf("nothing to pick: %s", prompt))
	}
	cmd := exec.Command("fzf", "--prompt", prompt+"> ", "--no-sort")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if _, ok := err.(*exec.ExitError); ok {
		os.Exit(1)
	}
	die(err)
	return strings.TrimSuffix(string(out), "\n")
}

var unsafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeName turns a sheet or tab title into a file name.
func safeName(s string) string {
	if s = strings.Trim(unsafeRe.ReplaceAllString(s, "_"), "_"); s != "" {
		return s
	}
	return "sheet"
}
