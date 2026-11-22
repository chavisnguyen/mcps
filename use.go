package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/chavisnguyen/mcps/internal/agents"
)

// useAgents picks the tools to rewrite for a folder: exactly the ones named
// (flag or .mcps.toml), else every installed tool plus any tool that already has
// a project file here, so old config is replaced rather than left behind.
func useAgents(csv, dir string) ([]agents.Agent, error) {
	if csv != "" {
		ags, _, err := pickAgents(csv)
		return ags, err
	}
	var out []agents.Agent
	for _, a := range agents.All() {
		p, ok := a.Path("local", dir)
		if ok && (a.Installed() || exists(p)) {
			out = append(out, a)
		}
	}
	return out, nil
}

// folderServers reads every MCP server currently configured for dir: the
// project-level file of each tool, plus Claude Code's per-project entries.
func folderServers(dir string, ags []agents.Agent) (map[string]*cand, []string, error) {
	found := map[string]*cand{}
	var notes []string
	empty := &state{Files: map[string][]string{}} // managed entries count too: they are what is on now
	for _, a := range ags {
		if p, ok := a.Path("local", dir); ok {
			if n := readInto(found, empty, a, p, dir); n != "" {
				notes = append(notes, n)
			}
		}
	}
	claudeProjects(found, []string{dir}, true)
	return found, notes, nil
}

// cmdUse: `mcps use` inside a project folder. Shows what the folder has now and
// what is in the hub, lets you choose, then rewrites the folder's MCP config.
func cmdUse(args []string) int {
	var c common
	var noSync, retools bool
	fs := newFlags("use", &c, nil, nil)
	fs.BoolVar(&noSync, "no-sync", false, "")
	fs.BoolVar(&retools, "tools", false, "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !isTTY() && !testPick {
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

	if len(r.Servers) == 0 {
		fmt.Println("Your hub is empty. First, bring in the MCP servers your AI tools already have.")
		fmt.Println()
		ia := []string{"--global"}
		if c.agents != "" {
			ia = append(ia, "--agents", c.agents)
		}
		if code := runImport(ia, true); code != 0 {
			return code
		}
		if r, err = loadRegistry(); err != nil {
			return fail(err)
		}
		fmt.Println()
	}

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
	csv := c.agents
	if csv == "" && (len(cur.Agents) == 0 || retools) {
		ids, err := chooseTools(dir, cur.Agents)
		if err != nil {
			return fail(err)
		}
		cur.Agents = ids
		fmt.Println()
	}
	if csv == "" {
		csv = strings.Join(cur.Agents, ",")
		names := make([]string, len(cur.Agents))
		for i, id := range cur.Agents {
			names[i] = agentName(id)
		}
		fmt.Println(dim("Tools: " + strings.Join(names, ", ") + "   (change: mcps use --tools)"))
	}
	ags, err := useAgents(csv, dir)
	if err != nil {
		return fail(err)
	}
	here, notes, err := folderServers(dir, ags)
	if err != nil {
		return fail(err)
	}
	for _, n := range notes {
		fmt.Println("!", n)
	}
	was := map[string]bool{}
	for _, n := range cur.Use {
		was[n] = true
	}

	var hub, only []pickItem
	hubNames := make([]string, 0, len(r.Servers))
	for n := range r.Servers {
		hubNames = append(hubNames, n)
	}
	sort.Strings(hubNames)
	for _, n := range hubNames {
		it := pickItem{ID: n, Name: n, Note: trunc(describe(r.Servers[n]), 60), On: was[n]}
		if cd := here[n]; cd != nil {
			it.On = true
			it.Note += "   · on in this folder now"
			if sp, err := r.resolve([]string{n}); err == nil && !sameSpec(sp[0], cd.spec()) {
				it.Note += " (differs; the hub version will be used)"
			}
		}
		hub = append(hub, it)
	}
	var onlyNames []string
	for n := range here {
		if _, in := r.Servers[n]; !in {
			onlyNames = append(onlyNames, n)
		}
	}
	sort.Strings(onlyNames)
	for _, n := range onlyNames {
		only = append(only, pickItem{ID: "only:" + n, Name: n, Note: trunc(summary(here[n].spec()), 60), On: true})
	}

	fmt.Println(bold("Step 2 · MCP servers") + "  " + dim(tilde(dir)))
	secs := []pickSection{{Title: fmt.Sprintf("In your hub (%d)", len(hub)), Items: hub}}
	if len(only) > 0 {
		secs = append(secs, pickSection{
			Title: fmt.Sprintf("Only in this folder (%d)  ·  ticking one adds it to your hub", len(only)), Items: only})
	}
	res, err := pickFn(secs, "↑↓ move · space tick · a all · n none · enter save · q cancel")
	if err != nil {
		return fail(err)
	}

	var chosen, promoteNames []string
	var promote []*agents.Spec
	for _, n := range hubNames {
		if res[n] {
			chosen = append(chosen, n)
		}
	}
	for _, n := range onlyNames {
		if res["only:"+n] {
			promoteNames = append(promoteNames, n)
			promote = append(promote, here[n].spec())
			chosen = append(chosen, n)
		}
	}
	sort.Strings(chosen)
	if len(chosen) == 0 && !exists(pf) && len(here) == 0 {
		fmt.Println("Nothing selected, so nothing was set up in this folder.")
		return 0
	}

	if len(promote) > 0 {
		bound := map[string]bool{}
		for _, n := range promoteNames {
			bound[n] = here[n].folderBound()
		}
		_, out, secrets, warns, err := prepareRegister(promote, bound)
		if err != nil {
			return fail(err)
		}
		if err := commitRegister(out, secrets); err != nil {
			return fail(err)
		}
		for _, w := range warns {
			fmt.Println("!", w)
		}
		if r, err = loadRegistry(); err != nil {
			return fail(err)
		}
	}

	cur.Use = chosen
	var b strings.Builder
	b.WriteString(projectHeader)
	if err := toml.NewEncoder(&b).Encode(cur); err != nil {
		return fail(err)
	}
	if err := writeAtomic(pf, b.String(), false); err != nil {
		return fail(err)
	}
	if noSync {
		fmt.Println("\nSaved your choice in .mcps.toml. Run mcps sync to apply it.")
		return 0
	}

	specs, err := r.resolve(chosen)
	if err != nil {
		return fail(err)
	}
	var results []result
	t := target{scope: "local", project: dir, agents: ags, quiet: true, res: &results, replace: true}
	failures := apply(t, specs, nil, true)
	summarize(dir, r, chosen, results)
	if len(promoteNames) > 0 {
		fmt.Printf("Added to your hub: %s\n", strings.Join(promoteNames, ", "))
	}
	if exists(backupRoot()) {
		fmt.Printf("Old files were backed up in %s\n", tilde(backupRoot()))
	}
	if failures > 0 {
		return 1
	}
	return 0
}

// testPick lets tests run cmdUse without a terminal.
var testPick bool

// chooseTools is step 1: which AI tools get config files in this folder.
func chooseTools(dir string, current []string) ([]string, error) {
	want := map[string]bool{}
	for _, id := range current {
		want[id] = true
	}
	var items []pickItem
	for _, a := range agents.All() {
		p, ok := a.Path("local", dir)
		if !ok {
			continue // e.g. Claude Desktop: no per-folder config
		}
		note, has := "not installed", exists(p)
		switch {
		case a.Installed() && has:
			note = "installed · has a file in this folder"
		case a.Installed():
			note = "installed"
		case has:
			note = "not installed · has a file in this folder"
		}
		on := a.Installed() || has
		if len(current) > 0 {
			on = want[a.ID()]
		}
		items = append(items, pickItem{ID: a.ID(), Name: agentName(a.ID()), Note: note, On: on})
	}
	fmt.Println(bold("Step 1 · Tools for this folder") + "  " + dim(tilde(dir)))
	res, err := pickFn([]pickSection{{Title: "Which AI tools should get MCP config here?", Items: items}},
		"↑↓ move · space tick · enter next · q cancel")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, it := range items {
		if res[it.ID] {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("pick at least one tool")
	}
	return ids, nil
}

// projectHeader is written at the top of .mcps.toml. It is also instructions for
// an AI agent working in this folder, so it can manage MCP servers without
// guessing (and without editing the generated files by hand).
const projectHeader = `# MCP servers for this folder, managed by mcps (https://github.com/chavisnguyen/mcps).
#
# For humans and AI agents:
#   - "use" below lists the servers this folder gets, by name. Names come from the hub: run  mcps list
#   - Change the list, then run  mcps sync  to rewrite the config for every AI tool.
#   - New server not in the hub yet? Add it once, then put its name in "use" and run  mcps sync:
#         mcps add NAME --url URL                       (remote server)
#         mcps add NAME -e KEY=value -- CMD ARG...      (local command; the key is stored privately)
#   - Do NOT edit the generated files by hand (.mcp.json, .codex/config.toml, .cursor/mcp.json,
#     .kiro/settings/mcp.json, opencode.json, .agents/mcp_config.json): the next sync overwrites them.
#   - "agents" (optional) limits which tools get files, e.g. agents = ["claude", "codex"].
#   - If something does not show up in a tool: mcps doctor
#   - "mcps use" is interactive (for humans); agents should edit this file and run mcps sync.
`
