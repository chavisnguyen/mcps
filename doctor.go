package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/chavisnguyen/mcps/internal/agents"
)

// loadProject reads <dir>/.mcps.toml.
func loadProject(dir string) (*Project, error) {
	var p Project
	md, err := toml.DecodeFile(filepath.Join(dir, ".mcps.toml"), &p)
	if err == nil {
		err = strictKeys(md)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ------------------------------------------------------------ drift check

// Drift states for one server in one agent's project file.
const (
	stOK      = "ok"
	stMissing = "missing"
	stDiffers = "differs"
)

type cell struct {
	Agent, Path, Server, State string
}

func sameSpec(a, b *agents.Spec) bool {
	eqMap := func(x, y map[string]string) bool {
		if len(x) == 0 && len(y) == 0 {
			return true
		}
		return reflect.DeepEqual(x, y)
	}
	if a.URL != b.URL || a.Command != b.Command || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return eqMap(a.Headers, b.Headers) && eqMap(a.Env, b.Env)
}

// projectAgents lists the agents a project should be synced to.
func projectAgents(p *Project) ([]agents.Agent, error) {
	ags, _, err := pickAgents(strings.Join(p.Agents, ","))
	return ags, err
}

// drift compares what .mcps.toml asks for with what is on disk, per agent.
func drift(r *Registry, dir string, p *Project) ([]cell, error) {
	specs, err := r.resolve(p.Use)
	if err != nil {
		return nil, err
	}
	ags, err := projectAgents(p)
	if err != nil {
		return nil, err
	}
	var out []cell
	for _, a := range ags {
		path, ok := a.Path("local", dir)
		if !ok {
			continue
		}
		have := map[string]*agents.Spec{}
		if text := readFile(path); text != "" {
			if got, err := a.Read(text); err == nil {
				for _, s := range got {
					have[s.Name] = s
				}
			}
		}
		for _, want := range specs {
			st := stOK
			switch g := have[want.Name]; {
			case g == nil:
				st = stMissing
			case !sameSpec(g, want):
				st = stDiffers
			}
			out = append(out, cell{a.ID(), path, want.Name, st})
		}
	}
	return out, nil
}

// knownProjects finds folders mcps has written to (from state.json) that still
// have a .mcps.toml.
func knownProjects() []string {
	seen := map[string]bool{}
	for file := range loadState().Files {
		for _, a := range agents.All() {
			marker, ok := a.Path("local", "\x00")
			if !ok {
				continue
			}
			suffix := strings.TrimPrefix(marker, "\x00")
			if strings.HasSuffix(file, suffix) {
				dir := strings.TrimSuffix(file, suffix)
				if exists(filepath.Join(dir, ".mcps.toml")) {
					seen[dir] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// cmdStatus: is every known folder in sync with the list?
func cmdStatus(args []string) int {
	var c common
	if err := newFlags("status", &c, nil, nil).Parse(args); err != nil {
		return 2
	}
	r, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	dirs := knownProjects()
	if cwd, _ := filepath.Abs(c.dir); exists(filepath.Join(cwd, ".mcps.toml")) {
		dirs = append([]string{cwd}, dropDir(dirs, cwd)...)
	}
	if len(dirs) == 0 {
		fmt.Println("No folders use mcps yet. Run `mcps use` inside a project folder to set one up.")
		return 0
	}
	bad := 0
	for _, dir := range dirs {
		p, err := loadProject(dir)
		if err != nil {
			bad++
			fmt.Printf("%s %s\n    cannot read .mcps.toml: %v\n", red("✗"), tilde(dir), err)
			continue
		}
		cells, err := drift(r, dir, p)
		if err != nil {
			bad++
			fmt.Printf("%s %s\n    %v\n", red("✗"), tilde(dir), err)
			continue
		}
		var probs []string
		for _, ce := range cells {
			if ce.State != stOK {
				probs = append(probs, fmt.Sprintf("%s: %s is %s", ce.Agent, ce.Server, ce.State))
			}
		}
		if len(probs) == 0 {
			fmt.Printf("%s %s  %s\n", green("✓"), tilde(dir), dim(fmt.Sprintf("%d server(s), in sync", len(p.Use))))
			continue
		}
		bad++
		fmt.Printf("%s %s\n", red("✗"), tilde(dir))
		for _, s := range probs {
			fmt.Println("    " + s)
		}
		fmt.Println("    " + dim("fix: mcps sync --dir "+dir))
	}
	if bad > 0 {
		return 1
	}
	return 0
}

func dropDir(in []string, d string) []string {
	var out []string
	for _, x := range in {
		if x != d {
			out = append(out, x)
		}
	}
	return out
}

// ------------------------------------------------------------------ doctor

type report struct{ bad, warn int }

func (r *report) ok(f string, a ...any) {
	fmt.Println("  " + green("✓") + " " + fmt.Sprintf(f, a...))
}
func (r *report) warnf(fix, f string, a ...any) {
	r.warn++
	fmt.Println("  " + yellow("!") + " " + fmt.Sprintf(f, a...))
	if fix != "" {
		fmt.Println("      " + dim("→ "+fix))
	}
}
func (r *report) fail(fix, f string, a ...any) {
	r.bad++
	fmt.Println("  " + red("✗") + " " + fmt.Sprintf(f, a...))
	if fix != "" {
		fmt.Println("      " + dim("→ "+fix))
	}
}

func heading(s string) { fmt.Println("\n" + bold(s)) }

// cmdDoctor explains why servers might not show up in an AI tool.
func cmdDoctor(args []string) int {
	var c common
	if err := newFlags("doctor", &c, nil, nil).Parse(args); err != nil {
		return 2
	}
	dir, _ := filepath.Abs(c.dir)
	var rep report

	fmt.Println(bold("Your server list"))
	r, err := loadRegistry()
	if err != nil {
		rep.fail("run `mcps use` to create it, or fix the file", "%v", err)
		return doctorSummary(&rep)
	}
	rep.ok("%s has %d server(s)", tilde(registryPath()), len(r.Servers))

	if fi, err := os.Stat(secretsPath()); err == nil {
		if fi.Mode().Perm()&0o077 != 0 {
			rep.warnf("chmod 600 "+tilde(secretsPath()), "%s can be read by other users", tilde(secretsPath()))
		} else {
			rep.ok("%s is private", tilde(secretsPath()))
		}
	}

	heading("This folder  " + dim(tilde(dir)))
	pf := filepath.Join(dir, ".mcps.toml")
	if !exists(pf) {
		fmt.Println("  " + dim("· this folder does not use mcps yet. Run `mcps use` here to choose servers."))
		return 0
	}
	p, err := loadProject(dir)
	if err != nil {
		rep.fail("fix .mcps.toml (see README)", "cannot read .mcps.toml: %v", err)
		return doctorSummary(&rep)
	}
	if len(p.Use) == 0 {
		rep.warnf("run `mcps use` and tick some servers", ".mcps.toml selects no servers")
		return doctorSummary(&rep)
	}
	var unknown []string
	for _, n := range p.Use {
		if _, ok := r.Servers[n]; !ok {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		rep.fail("run `mcps add "+unknown[0]+" ...` or remove it from .mcps.toml",
			"not in your list: %s", strings.Join(unknown, ", "))
		return doctorSummary(&rep)
	}
	rep.ok("uses %d server(s): %s", len(p.Use), strings.Join(p.Use, ", "))

	specs, err := r.resolve(p.Use)
	if err != nil {
		rep.fail("", "%s", strings.ReplaceAll(err.Error(), "\n  ", "\n      → "))
		return doctorSummary(&rep)
	}

	heading("Servers")
	for _, s := range specs {
		checkServer(&rep, s)
	}

	heading("AI tools")
	cells, err := drift(r, dir, p)
	if err != nil {
		rep.fail("", "%v", err)
		return doctorSummary(&rep)
	}
	byAgent := map[string][]cell{}
	var order []string
	for _, ce := range cells {
		if _, ok := byAgent[ce.Agent]; !ok {
			order = append(order, ce.Agent)
		}
		byAgent[ce.Agent] = append(byAgent[ce.Agent], ce)
	}
	if len(order) == 0 {
		rep.warnf("install a supported tool, or pass --agents", "no supported AI tool detected")
	}
	for _, id := range order {
		var probs []string
		for _, ce := range byAgent[id] {
			if ce.State != stOK {
				probs = append(probs, ce.Server+" "+ce.State)
			}
		}
		name := agentNames[id]
		if name == "" {
			name = id
		}
		if len(probs) > 0 {
			rep.fail("run `mcps sync` in this folder", "%s: %s (%s)", name, strings.Join(probs, ", "), tilde(byAgent[id][0].Path))
			continue
		}
		rep.ok("%s: all servers present in %s", name, rel(dir, byAgent[id][0].Path))
		if h := agentHint(id); h != "" {
			fmt.Println("      " + dim("note: "+h))
		}
	}
	gitCheck(&rep, dir, byAgent, specs)
	return doctorSummary(&rep)
}

func agentHint(id string) string {
	switch id {
	case "claude":
		return "Claude Code asks you to approve project servers the first time; restart it after a sync."
	case "codex":
		return "Codex only reads project config for trusted projects."
	case "cursor", "kiro", "opencode", "agy":
		return "restart the tool (or reload MCP servers) after a sync."
	}
	return ""
}

func doctorSummary(r *report) int {
	fmt.Println()
	switch {
	case r.bad > 0:
		fmt.Println(red(fmt.Sprintf("%d problem(s)", r.bad)) + fmt.Sprintf(", %d warning(s). Fix the ✗ lines above, then run `mcps doctor` again.", r.warn))
		return 1
	case r.warn > 0:
		fmt.Println(green("No problems") + fmt.Sprintf(", %d warning(s).", r.warn))
	default:
		fmt.Println(green("All good."))
	}
	return 0
}

// checkServer verifies a local command exists, or a localhost URL is listening.
func checkServer(rep *report, s *agents.Spec) {
	if s.Remote() {
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" {
			rep.fail("fix the url in your list", "%s: %q is not a valid url", s.Name, s.URL)
			return
		}
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			rep.ok("%s: remote server %s (not probed)", s.Name, u.Host)
			return
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443"}[u.Scheme]
			if port == "" {
				port = "80"
			}
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 700*time.Millisecond)
		if err != nil {
			rep.warnf("start that server first (its own start command)", "%s: nothing is listening on %s:%s", s.Name, host, port)
			return
		}
		conn.Close()
		rep.ok("%s: %s:%s is up", s.Name, host, port)
		return
	}
	if _, err := exec.LookPath(s.Command); err != nil {
		if filepath.IsAbs(s.Command) && exists(s.Command) {
			rep.ok("%s: runs %s", s.Name, tilde(s.Command))
			return
		}
		rep.fail("install it, or fix cmd in your list", "%s: command %q not found on PATH", s.Name, s.Command)
		return
	}
	rep.ok("%s: command %q found", s.Name, s.Command)
}

// gitCheck warns (read-only) when a generated file that holds a secret is not
// ignored by git. It never changes anything.
func gitCheck(rep *report, dir string, byAgent map[string][]cell, specs []*agents.Spec) {
	hasSecret := false
	for _, s := range specs {
		if len(s.Env) > 0 || len(s.Headers) > 0 {
			hasSecret = true
		}
	}
	if !hasSecret {
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run() != nil {
		return
	}
	heading("Git")
	any := false
	for _, cs := range byAgent {
		p := cs[0].Path
		if !exists(p) {
			continue
		}
		if exec.Command("git", "-C", dir, "check-ignore", "-q", p).Run() != nil {
			any = true
			rep.warnf("add it to .gitignore if the repo is shared", "%s holds API keys and is not ignored by git", rel(dir, p))
		}
	}
	if !any {
		rep.ok("generated files holding keys are ignored by git")
	}
}
