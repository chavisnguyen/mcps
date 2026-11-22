package main

import (
	"flag"
	"fmt"
	"github.com/chavisnguyen/mcps/internal/agents"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

var version = "dev"

const usage = `mcps - set up MCP servers for all your AI tools, from one list.

Quick start
  Run "mcps" inside a project folder. It walks you through it.

Everyday
  mcps                         set up MCP servers for the current folder (guided)
  mcps add NAME --url URL      add a new MCP server to the mcps list
  mcps add NAME -- CMD ARG...  ...or a local one, e.g.  mcps add github -- npx -y @modelcontextprotocol/server-github
  mcps remove NAME             remove a server from the mcps list
  mcps list                    show the mcps list
  mcps import                  add MCP servers already set up in your AI tools to the list

More
  mcps sync                    re-apply this folder's .mcps.toml (after editing it by hand)
  mcps doctor                  find out why a server is not showing up in a tool
  mcps status                  is every folder you set up still in sync with the list?
  mcps validate [file...]      check the list and project files; changes nothing
  mcps up <server>             run a server's "install" command from the list
  mcps init [git-url]          create the list (done for you), or use a git repo as the list
  mcps global add|rm|list      edit your AI tools' global config directly
  mcps sync -g                 apply [global] use from the list to your AI tools' global config

Flags: --dry-run  --agents claude,codex  --dir PATH  --local (global add/rm/list)  -e KEY[=VALUE]  -H "Name: value"
Env:   MCPS_HOME (default ~/.mcps), MCPS_REGISTRY (path to servers.toml)
`

type envFlag map[string]string

func (e envFlag) String() string { return "" }
func (e envFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		var set bool
		if val, set = os.LookupEnv(k); !set {
			return fmt.Errorf("-e %s: no value and $%s is not set", k, k)
		}
	}
	e[k] = val
	return nil
}

type common struct {
	local, global, dry bool
	dir, agents        string
}

func newFlags(name string, c *common, env envFlag, url *string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.BoolVar(&c.local, "local", false, "")
	fs.BoolVar(&c.global, "g", false, "")
	fs.BoolVar(&c.dry, "dry-run", false, "")
	fs.StringVar(&c.dir, "dir", ".", "")
	fs.StringVar(&c.agents, "agents", "", "")
	if env != nil {
		fs.Var(env, "e", "")
	}
	if url != nil {
		fs.StringVar(url, "url", "", "")
	}
	return fs
}

func (c *common) target(scope string) (target, bool, error) {
	ags, explicit, err := pickAgents(c.agents)
	if err != nil {
		return target{}, false, err
	}
	proj, _ := filepath.Abs(c.dir)
	return target{scope: scope, project: proj, agents: ags, dry: c.dry}, explicit, nil
}

func header(t target, what string) {
	loc := t.scope
	if t.scope == "local" {
		loc += " @ " + t.project
	}
	dry := ""
	if t.dry {
		dry = "  (dry-run)"
	}
	fmt.Printf("[%s] %s%s\n", loc, what, dry)
}

func footer(t target, explicit bool) {
	if !explicit {
		have := map[string]bool{}
		for _, a := range t.agents {
			have[a.ID()] = true
		}
		var skipped []string
		for _, a := range agents.All() {
			if !have[a.ID()] {
				skipped = append(skipped, a.ID())
			}
		}
		if len(skipped) > 0 {
			fmt.Printf("  (not detected, skipped: %s; force with --agents)\n", strings.Join(skipped, ", "))
		}
	}
	if !t.dry && exists(backupRoot()) {
		fmt.Printf("  backups: %s\n", backupRoot())
	}
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "error:", err)
	return 1
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		if isTTY() {
			return cmdPick(nil)
		}
		fmt.Print(usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	case "version", "--version":
		fmt.Println("mcps", version)
		return 0
	case "init":
		return cmdInit(rest)
	case "servers":
		return cmdServers()
	case "up":
		return cmdUp(rest)
	case "import":
		return cmdImport(rest)
	case "pick", "ui":
		return cmdPick(rest)
	case "sync":
		return cmdSync(rest)
	case "validate":
		return cmdValidate(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "status":
		return cmdStatus(rest)
	case "add":
		return cmdAdd(rest)
	case "rm", "remove":
		return cmdRemove(rest)
	case "list", "ls":
		return cmdServers()
	case "global":
		return cmdGlobal(rest)
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
	return 2
}

// ------------------------------------------------------------------ commands

const registryTemplate = `# mcps registry: every MCP server you have, declared once.
# Projects pick from this list: run mcps inside a project folder.
# Never write secrets here; reference environment variables as "${NAME}".

# [servers.github]                       # local process
# cmd = ["npx", "-y", "@modelcontextprotocol/server-github"]
# env = { GITHUB_PERSONAL_ACCESS_TOKEN = "${GITHUB_TOKEN}" }

# [servers.browser]                      # remote server (already running)
# url = "http://localhost:3025/mcp"

# [servers.composio]                     # remote server with an auth header
# url = "https://example.com/mcp"
# headers = { x-api-key = "${EXAMPLE_API_KEY}" }

# Same server, two accounts = two instances with different names:
# [servers.slack-a]
# url = "http://localhost:3010/mcp"
# [servers.slack-b]
# url = "http://localhost:3011/mcp"
`

// scaffoldRegistry creates an empty, commented registry if none exists yet.
func scaffoldRegistry() (path string, created bool, err error) {
	path = registryPath()
	if exists(path) {
		return path, false, nil
	}
	return path, true, writeAtomic(path, registryTemplate, false)
}

func cmdInit(args []string) int {
	if len(args) == 0 {
		p, created, err := scaffoldRegistry()
		if err != nil {
			return fail(err)
		}
		if created {
			fmt.Println("created", p)
			fmt.Println("add your MCP servers there, then run mcps inside a project folder")
		} else {
			fmt.Println("registry already exists:", p)
		}
		return 0
	}
	if len(args) != 1 {
		return fail(fmt.Errorf("usage: mcps init [git-url]"))
	}
	dir := configDir()
	var c *exec.Cmd
	if exists(filepath.Join(dir, ".git")) {
		c = exec.Command("git", "-C", dir, "pull", "--ff-only")
	} else {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return fail(err)
		}
		c = exec.Command("git", "clone", args[0], dir)
	}
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fail(fmt.Errorf("git: %w (private repo? check your ssh key / gh auth)", err))
	}
	fmt.Println("config ready:", dir)
	return 0
}

func cmdServers() int {
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	var names []string
	for n := range r.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := r.Servers[n]
		desc := d.URL
		if d.URL == "" {
			desc = strings.Join(d.Cmd, " ")
		}
		fmt.Printf("  %-20s %s\n", n, desc)
	}
	return 0
}

func cmdUp(args []string) int {
	if len(args) != 1 {
		return fail(fmt.Errorf("usage: mcps up <server>"))
	}
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	d, ok := r.Servers[args[0]]
	if !ok {
		return fail(fmt.Errorf("server %q is not defined in the registry", args[0]))
	}
	if d.Install == "" {
		return fail(fmt.Errorf("server %q has no install command", args[0]))
	}
	fmt.Println("$", d.Install)
	c := exec.Command("sh", "-c", d.Install)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fail(err)
	}
	return 0
}

func cmdSync(args []string) int {
	var c common
	if err := newFlags("sync", &c, nil, nil).Parse(args); err != nil {
		return 2
	}
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	scope, use := "global", r.Global.Use
	var projAgents []string
	if !c.global {
		scope = "local"
		pf := filepath.Join(c.dir, ".mcps.toml")
		var p Project
		md, err := toml.DecodeFile(pf, &p)
		if err == nil {
			err = strictKeys(md)
		}
		if err != nil {
			return fail(fmt.Errorf("%s: %w (create it with: use = [\"name\", ...], or use -g for global)", pf, err))
		}
		use, projAgents = p.Use, p.Agents
	}
	if c.agents == "" && len(projAgents) > 0 {
		c.agents = strings.Join(projAgents, ",")
	}
	specs, err := r.resolve(use)
	if err != nil {
		return fail(err)
	}
	t, explicit, err := c.target(scope)
	if err != nil {
		return fail(err)
	}
	header(t, fmt.Sprintf("sync %d server(s): %s", len(use), strings.Join(use, ", ")))
	n := apply(t, specs, nil, true)
	footer(t, explicit)
	if n > 0 {
		return 1
	}
	return 0
}

func scopeOf(c *common) string {
	if c.local {
		return "local"
	}
	return "global"
}

func cmdGlobalAdd(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fail(fmt.Errorf("usage: mcps global add NAME [flags] (--url URL | -- CMD ARG...)"))
	}
	name := args[0]
	flags, cmdline := args[1:], []string(nil)
	for i, a := range flags {
		if a == "--" {
			flags, cmdline = flags[:i], flags[i+1:]
			break
		}
	}
	var c common
	var url string
	env := envFlag{}
	if err := newFlags("add", &c, env, &url).Parse(flags); err != nil {
		return 2
	}
	if (url == "") == (len(cmdline) == 0) {
		return fail(fmt.Errorf("give either --url URL or a command after `--`"))
	}
	s := &agents.Spec{Name: name, URL: url}
	if url == "" {
		s.Command, s.Args = cmdline[0], cmdline[1:]
		if len(env) > 0 {
			s.Env = env
			fmt.Fprintln(os.Stderr, "note: env values are written in plaintext into each agent's config")
		}
	}
	t, explicit, err := c.target(scopeOf(&c))
	if err != nil {
		return fail(err)
	}
	header(t, fmt.Sprintf("set '%s'", name))
	n := apply(t, nil, []op{{name, s}}, false)
	footer(t, explicit)
	if n > 0 {
		return 1
	}
	return 0
}

func cmdGlobalRm(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fail(fmt.Errorf("usage: mcps global rm NAME [flags]"))
	}
	var c common
	if err := newFlags("rm", &c, nil, nil).Parse(args[1:]); err != nil {
		return 2
	}
	t, explicit, err := c.target(scopeOf(&c))
	if err != nil {
		return fail(err)
	}
	header(t, fmt.Sprintf("remove '%s'", args[0]))
	n := apply(t, nil, []op{{args[0], nil}}, false)
	footer(t, explicit)
	if n > 0 {
		return 1
	}
	return 0
}

func cmdGlobalList(args []string) int {
	var c common
	if err := newFlags("list", &c, nil, nil).Parse(args); err != nil {
		return 2
	}
	t, _, err := c.target(scopeOf(&c))
	if err != nil {
		return fail(err)
	}
	header(t, "list")
	rows := map[string]map[string]bool{}
	var ids []string
	for _, a := range t.agents {
		p, ok := a.Path(t.scope, t.project)
		if !ok {
			continue
		}
		ids = append(ids, a.ID())
		names, err := a.Names(readFile(p))
		if err != nil {
			fmt.Printf("  ! %s: %v\n", a.ID(), err)
			continue
		}
		for _, n := range names {
			if rows[n] == nil {
				rows[n] = map[string]bool{}
			}
			rows[n][a.ID()] = true
		}
	}
	if len(rows) == 0 {
		fmt.Println("  (none)")
		return 0
	}
	var names []string
	w := 6
	for n := range rows {
		names = append(names, n)
		w = max(w, len(n))
	}
	sort.Strings(names)
	fmt.Printf("  %-*s ", w, "server")
	for _, id := range ids {
		fmt.Printf(" %-8.8s", id)
	}
	fmt.Println()
	for _, n := range names {
		fmt.Printf("  %-*s ", w, n)
		for _, id := range ids {
			m := "."
			if rows[n][id] {
				m = "x"
			}
			fmt.Printf(" %-8s", m)
		}
		fmt.Println()
	}
	return 0
}

func cmdValidate(args []string) int {
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	bad := 0
	check := func(label string, use []string) {
		for _, n := range use {
			if _, ok := r.Servers[n]; !ok {
				fmt.Printf("  ! %s: unknown server %q\n", label, n)
				bad++
			}
		}
	}
	check("[global] use", r.Global.Use)
	unset := map[string]bool{}
	for n, d := range r.Servers {
		parts := append([]string{d.URL}, d.Cmd...)
		for _, v := range d.Headers {
			parts = append(parts, v)
		}
		for _, v := range d.Env {
			parts = append(parts, v)
		}
		for _, p := range parts {
			for _, m := range envRef.FindAllStringSubmatch(p, -1) {
				if _, ok := lookupVar(m[1]); !ok {
					unset[m[1]+" (used by "+n+")"] = true
				}
			}
		}
	}
	if len(args) == 0 && exists(".mcps.toml") {
		args = []string{".mcps.toml"}
	}
	for _, f := range args {
		var p Project
		md, err := toml.DecodeFile(f, &p)
		if err == nil {
			err = strictKeys(md)
		}
		if err != nil {
			fmt.Printf("  ! %s: %v\n", f, err)
			bad++
			continue
		}
		check(f, p.Use)
	}
	var warns []string
	for w := range unset {
		warns = append(warns, w)
	}
	sort.Strings(warns)
	for _, w := range warns {
		fmt.Printf("  ? env not set in this shell: %s\n", w)
	}
	if bad > 0 {
		return 1
	}
	fmt.Printf("ok: %d server(s) in registry\n", len(r.Servers))
	return 0
}

func cmdGlobal(args []string) int {
	if len(args) == 0 {
		return fail(fmt.Errorf("usage: mcps global add|rm|list ..."))
	}
	switch args[0] {
	case "add":
		return cmdGlobalAdd(args[1:])
	case "rm", "remove":
		return cmdGlobalRm(args[1:])
	case "list", "ls":
		return cmdGlobalList(args[1:])
	}
	return fail(fmt.Errorf("unknown: mcps global %s", args[0]))
}
