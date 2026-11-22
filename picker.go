package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chavisnguyen/mcps/internal/agents"
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
	fmt.Println("Run mcps use again any time to change which MCP servers this folder uses.")
}

func trunc(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

var _ = agents.All
