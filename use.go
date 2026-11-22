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

// folderServers reads every MCP server currently configured for dir: the
// project-level file of each tool, plus Claude Code's per-project entries.
func folderServers(dir, csv string) (map[string]*cand, []string, error) {
	ags := agents.All()
	if csv != "" {
		var err error
		if ags, _, err = pickAgents(csv); err != nil {
			return nil, nil, err
		}
	}
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
	var noSync bool
	fs := newFlags("use", &c, nil, nil)
	fs.BoolVar(&noSync, "no-sync", false, "")
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
	here, notes, err := folderServers(dir, c.agents)
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

	fmt.Println(bold("This folder") + "  " + dim(tilde(dir)))
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
	b.WriteString("# managed by mcps; safe to edit by hand, then run: mcps sync\n")
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
	csv := c.agents
	if csv == "" && len(cur.Agents) > 0 {
		csv = strings.Join(cur.Agents, ",")
	}
	ags, _, err := pickAgents(csv)
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
