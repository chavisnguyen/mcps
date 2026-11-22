package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"atomicgo.dev/keyboard/keys"
	"github.com/BurntSushi/toml"
	"github.com/chavisnguyen/mcps/internal/agents"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// The checklists use pterm: raw mode, key parsing, redraw and cleanup are the
// library's job. (Bubble Tea/huh was rejected: its init() queries the terminal's
// background colour at process start, which stalls ~5s on terminals that never
// answer, for every mcps command.)

func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

var agentNames = map[string]string{
	"claude": "Claude Code", "codex": "Codex", "cursor": "Cursor", "kiro": "Kiro",
	"agy": "AGY", "opencode": "OpenCode", "desktop": "Claude Desktop",
}

func agentName(id string) string {
	if n, ok := agentNames[id]; ok {
		return n
	}
	return id
}

// diffSelection compares the previous `use` list with the new one.
func diffSelection(was map[string]bool, chosen []string) (added, removed []string) {
	now := map[string]bool{}
	for _, n := range chosen {
		now[n] = true
		if !was[n] {
			added = append(added, n)
		}
	}
	for n := range was {
		if !now[n] {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return
}

func describe(d ServerDef) string {
	if d.URL != "" {
		return d.URL
	}
	return strings.Join(d.Cmd, " ")
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return strings.Join(s, ", ")
}

func rel(dir, p string) string {
	if r, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// cmdPick is the guided flow: `mcps` inside a project folder.
func cmdPick(args []string) int {
	var c common
	var noSync bool
	fs := newFlags("pick", &c, nil, nil)
	fs.BoolVar(&noSync, "no-sync", false, "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !isTTY() {
		return fail(errors.New("this needs an interactive terminal; edit .mcps.toml and run `mcps sync` instead"))
	}
	if _, _, err := scaffoldRegistry(); err != nil {
		return fail(err)
	}
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	dir, _ := filepath.Abs(c.dir)
	pf := filepath.Join(dir, ".mcps.toml")

	fmt.Println("mcps keeps all your MCP servers in one list and sets them up for every AI tool you use.")
	fmt.Println()

	firstRun := len(r.Servers) == 0
	if firstRun {
		fmt.Println("Step 1/2 · Your MCP servers")
		ia := []string{}
		if c.agents != "" {
			ia = append(ia, "--agents", c.agents)
		}
		if code := runImport(ia, true); code != 0 {
			return code
		}
		if r, err = loadRegistry(); err != nil {
			return fail(err)
		}
		if len(r.Servers) == 0 {
			fmt.Printf("\nThere are no MCP servers in the mcps list yet, so there is nothing to set up.\n")
			fmt.Printf("Add some by editing %s (examples are inside), or run mcps again.\n", tilde(registryPath()))
			return 0
		}
		fmt.Println()
		fmt.Println("Step 2/2 · This folder")
	} else {
		fmt.Println("This folder")
	}
	fmt.Println(tilde(dir))

	var cur Project
	if exists(pf) {
		md, err := toml.DecodeFile(pf, &cur)
		if err == nil {
			err = strictKeys(md)
		}
		if err != nil {
			return fail(fmt.Errorf("%s: %w", pf, err))
		}
	}
	was := map[string]bool{}
	for _, n := range cur.Use {
		was[n] = true
	}

	names := make([]string, 0, len(r.Servers))
	w := 0
	for n := range r.Servers {
		names = append(names, n)
		w = max(w, len(n))
	}
	sort.Strings(names)
	labels := make([]string, len(names))
	byLabel := map[string]string{}
	var defaults []string
	for i, n := range names {
		labels[i] = fmt.Sprintf("%-*s  %s", w, n, trunc(describe(r.Servers[n]), 70))
		byLabel[labels[i]] = n
		if was[n] {
			defaults = append(defaults, labels[i])
		}
	}
	_, th, _ := term.GetSize(int(os.Stdout.Fd()))
	if th <= 0 {
		th = 24
	}
	fmt.Println("Which MCP servers should this project use?")
	fmt.Println("↑/↓ move · space tick/untick · ←/→ none/all · enter confirm · ctrl+c cancel")
	picked, err := pterm.DefaultInteractiveMultiselect.
		WithOptions(labels).
		WithDefaultOptions(defaults).
		WithFilter(false).
		WithKeySelect(keys.Space).
		WithKeyConfirm(keys.Enter).
		WithMaxHeight(max(min(len(labels), th-10), 3)).
		Show("Servers for this folder")
	if err != nil {
		return fail(err)
	}
	chosen := make([]string, 0, len(picked))
	for _, l := range picked {
		chosen = append(chosen, byLabel[l])
	}
	sort.Strings(chosen)
	if len(chosen) == 0 && !exists(pf) {
		fmt.Println("\nNothing selected, so nothing was set up in this folder.")
		return 0
	}

	for n := range was {
		if _, ok := r.Servers[n]; !ok {
			fmt.Printf("Note: %q is no longer in the mcps list and was dropped from this folder.\n", n)
		}
	}
	cur.Use = chosen
	var b bytes.Buffer
	b.WriteString("# managed by mcps; safe to edit by hand, then run: mcps sync\n")
	if err := toml.NewEncoder(&b).Encode(cur); err != nil {
		return fail(err)
	}
	if err := writeAtomic(pf, b.String(), false); err != nil {
		return fail(err)
	}
	if noSync {
		fmt.Printf("\nSaved your choice in .mcps.toml. Run mcps sync to apply it.\n")
		return 0
	}

	specs, err := r.resolve(chosen)
	if err != nil {
		return fail(err)
	}
	csv := c.agents
	if csv == "" && len(cur.Agents) > 0 {
		csv = strings.Join(cur.Agents, ",")
	}
	ags, _, err := pickAgents(csv)
	if err != nil {
		return fail(err)
	}
	var res []result
	t := target{scope: "local", project: dir, agents: ags, quiet: true, res: &res}
	failures := apply(t, specs, nil, true)
	summarize(dir, r, chosen, res)
	if failures > 0 {
		return 1
	}
	return 0
}

// summarize prints the outcome in plain words.
func summarize(dir string, r *Registry, chosen []string, res []result) {
	var wrote, tools, skipped []string
	var problems []string
	changed := false
	seen := map[string]bool{}
	for _, x := range res {
		switch x.status {
		case "written":
			changed = true
			wrote = append(wrote, rel(dir, x.path))
			if !seen[x.agent] {
				tools = append(tools, agentName(x.agent))
				seen[x.agent] = true
			}
		case "skipped":
			skipped = append(skipped, agentName(x.agent))
		case "error":
			problems = append(problems, fmt.Sprintf("Could not update %s: %s", agentName(x.agent), x.detail))
		}
	}
	fmt.Println()
	switch {
	case changed && len(chosen) > 0:
		fmt.Printf("Done. %d MCP server(s) set up for this folder: %s\n", len(chosen), strings.Join(chosen, ", "))
		fmt.Printf("Set up for: %s\n", strings.Join(tools, ", "))
		fmt.Println("Files created or updated:")
		for _, f := range wrote {
			fmt.Println("  ✓", f)
		}
	case changed:
		fmt.Println("Done. Removed the MCP servers mcps had set up in this folder.")
		for _, f := range wrote {
			fmt.Println("  ✓", f)
		}
	case len(problems) == 0:
		fmt.Println("Nothing to change: this folder already has these MCP servers set up.")
	}
	for _, p := range problems {
		fmt.Println("!", p)
	}
	if len(skipped) > 0 {
		fmt.Printf("Skipped: %s (no per-folder config)\n", strings.Join(skipped, ", "))
	}
	secret := false
	for _, n := range chosen {
		if d := r.Servers[n]; len(d.Env) > 0 || len(d.Headers) > 0 {
			secret = true
		}
	}
	if secret && changed {
		fmt.Println("Note: these files can contain API keys; add them to .gitignore if needed.")
	}
	fmt.Println("Run mcps again any time to change which MCP servers this folder uses.")
}

func trunc(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

var _ = agents.All
