package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"atomicgo.dev/keyboard/keys"
	"github.com/BurntSushi/toml"
	"github.com/chavisnguyen/mcps/internal/agents"
	"github.com/pterm/pterm"
)

// import scans the GLOBAL MCP config of every supported agent found on this
// machine, lets you choose which servers to bring into the registry, and
// appends them to servers.toml. Nothing is removed from the agents' own files.

type seen struct {
	agent string
	where string // project folder; "" = the tool's global config
	spec  *agents.Spec
}

type cand struct {
	name    string
	from    []seen
	differs []string // agents whose entry differs from the first one
}

func (c *cand) spec() *agents.Spec { return c.from[0].spec }

func (c *cand) agentIDs() string {
	ids := make([]string, len(c.from))
	for i, f := range c.from {
		ids[i] = agentName(f.agent)
		if f.where != "" {
			ids[i] += " in " + tilde(f.where)
		}
	}
	return strings.Join(ids, ", ")
}

// folderBound reports whether a project-level server points at files that
// exist relative to its own folder (so it would break when used elsewhere).
func (c *cand) folderBound() bool {
	for _, f := range c.from {
		if f.where == "" {
			continue
		}
		for _, a := range append([]string{f.spec.Command}, f.spec.Args...) {
			if a != "" && !filepath.IsAbs(a) && strings.ContainsAny(a, "/.") && !strings.HasPrefix(a, "-") &&
				!strings.HasPrefix(a, "@") && exists(filepath.Join(f.where, a)) {
				return true
			}
		}
	}
	return false
}

func canon(s *agents.Spec) string {
	b, _ := json.Marshal(struct {
		U, C string
		A    []string
		E, H map[string]string
	}{s.URL, s.Command, s.Args, s.Env, s.Headers})
	return string(b)
}

// addFound records one server seen in one place, merging same-named entries.
func addFound(found map[string]*cand, s *agents.Spec, agent, where string) {
	c := found[s.Name]
	if c == nil {
		c = &cand{name: s.Name}
		found[s.Name] = c
	} else if canon(c.spec()) != canon(s) {
		c.differs = append(c.differs, agent)
	}
	c.from = append(c.from, seen{agent, where, s})
}

// readInto reads one config file and records every server in it that mcps
// did not generate itself (tracked in state.json).
func readInto(found map[string]*cand, st *state, a agents.Agent, path, where string) string {
	if !exists(path) {
		return ""
	}
	specs, err := a.Read(readFile(path))
	if err != nil {
		return fmt.Sprintf("%s: could not read %s: %v", a.ID(), tilde(path), err)
	}
	managed := map[string]bool{}
	for _, n := range st.Files[path] {
		managed[n] = true
	}
	for _, s := range specs {
		if !managed[s.Name] {
			addFound(found, s, a.ID(), where)
		}
	}
	return ""
}

// scanGlobal reads each agent's global file, plus (optionally) project-level
// files: in every folder under each root in scan, and Claude Code's per-project
// entries stored in ~/.claude.json.
func scanGlobal(csv string, withGlobal bool, here string, scan []string) (map[string]*cand, []string, error) {
	ags, _, err := pickAgents(csv)
	if err != nil {
		return nil, nil, err
	}
	st := loadState()
	found := map[string]*cand{}
	var notes []string
	for _, a := range ags {
		if !withGlobal {
			break
		}
		if path, ok := a.Path("global", ""); ok {
			if n := readInto(found, st, a, path, ""); n != "" {
				notes = append(notes, n)
			}
		}
	}
	var dirs []string
	if here != "" {
		dirs = append(dirs, here)
	}
	for _, root := range scan {
		dirs = append(dirs, walkProjects(root, ags)...)
	}
	for _, d := range dirs {
		for _, a := range ags {
			if path, ok := a.Path("local", d); ok {
				if n := readInto(found, st, a, path, d); n != "" {
					notes = append(notes, n)
				}
			}
		}
	}
	claudeProjects(found, append([]string{here}, scan...))
	return found, notes, nil
}

var skipDirs = map[string]bool{"node_modules": true, ".git": true, "vendor": true, "Library": true,
	".Trash": true, "target": true, "dist": true, "build": true, ".venv": true, "venv": true}

// walkProjects returns folders under root (max depth 5) that hold a
// project-level MCP file of any given agent.
func walkProjects(root string, ags []agents.Agent) []string {
	var out []string
	rootDepth := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && (skipDirs[d.Name()] || strings.Count(p, string(os.PathSeparator))-rootDepth > 5) {
			return filepath.SkipDir
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir // hidden folders are never projects themselves
		}
		for _, a := range ags {
			if f, ok := a.Path("local", p); ok && exists(f) {
				out = append(out, p)
				break
			}
		}
		return nil
	})
	return out
}

// claudeProjects reads Claude Code's "local scope" servers (kept per project
// inside ~/.claude.json) for the scanned folders.
func claudeProjects(found map[string]*cand, scan []string) {
	var doc struct {
		Projects map[string]struct {
			MCP map[string]json.RawMessage `json:"mcpServers"`
		} `json:"projects"`
	}
	if json.Unmarshal([]byte(readFile(filepath.Join(home(), ".claude.json"))), &doc) != nil {
		return
	}
	for dir, pr := range doc.Projects {
		in := false
		for _, root := range scan {
			if root == "" {
				continue
			}
			if dir == root || strings.HasPrefix(dir, filepath.Clean(root)+string(os.PathSeparator)) {
				in = true
			}
		}
		if !in {
			continue
		}
		for name, raw := range pr.MCP {
			if s := agents.SpecFromJSON(name, raw); s != nil {
				addFound(found, s, "claude", dir)
			}
		}
	}
}

// ---------------------------------------------------------------- conversion

var secretish = regexp.MustCompile(`(?i)(token|secret|passw|api[_-]?key|apikey|key|auth|cookie|credential|bearer)`)
var urlCred = regexp.MustCompile(`(?i)[?&](key|token|api_?key|secret|access_token)=`)
var nonAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

func varName(server, key string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToUpper(server+"_"+key), "_"), "_")
}

// toDef turns a Spec into a registry entry. Values that look like secrets are
// replaced by ${VAR}; the real value is returned in secrets.
func toDef(s *agents.Spec) (def ServerDef, secrets map[string]string, warns []string) {
	secrets = map[string]string{}
	conv := func(in map[string]string) map[string]string {
		if len(in) == 0 {
			return nil
		}
		out := map[string]string{}
		for k, v := range in {
			if secretish.MatchString(k) && v != "" && !envRef.MatchString(v) {
				vn := varName(s.Name, k)
				secrets[vn] = v
				out[k] = "${" + vn + "}"
			} else {
				out[k] = v
			}
		}
		return out
	}
	def.URL = s.URL
	if s.Command != "" {
		def.Cmd = append([]string{s.Command}, s.Args...)
	}
	def.Env, def.Headers = conv(s.Env), conv(s.Headers)
	if urlCred.MatchString(s.URL) {
		warns = append(warns, s.Name+": URL has a credential-like query parameter, copied as-is")
	}
	return
}

func q(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

var bare = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tk(k string) string {
	if bare.MatchString(k) {
		return k
	}
	return q(k)
}

func inline(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tk(k) + " = " + q(m[k])
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func renderServer(name string, d ServerDef) string {
	var b strings.Builder
	b.WriteString("[servers." + tk(name) + "]\n")
	if d.URL != "" {
		b.WriteString("url = " + q(d.URL) + "\n")
	}
	if len(d.Cmd) > 0 {
		qs := make([]string, len(d.Cmd))
		for i, c := range d.Cmd {
			qs[i] = q(c)
		}
		b.WriteString("cmd = [" + strings.Join(qs, ", ") + "]\n")
	}
	if len(d.Env) > 0 {
		b.WriteString("env = " + inline(d.Env) + "\n")
	}
	if len(d.Headers) > 0 {
		b.WriteString("headers = " + inline(d.Headers) + "\n")
	}
	return b.String()
}

func summary(s *agents.Spec) string {
	if s.URL != "" {
		return s.URL
	}
	return strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
}

// ------------------------------------------------------------------ command

type multiFlag []string

func (m *multiFlag) String() string     { return "" }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdImport(args []string) int { return runImport(args, false) }

// runImport; guided=true when called from the step-by-step flow (it continues
// to the next step, so the closing hint is omitted).
func runImport(args []string, guided bool) int {
	var c common
	var yes bool
	fs := newFlags("import", &c, nil, nil)
	var scan multiFlag
	fs.BoolVar(&yes, "yes", false, "")
	fs.Var(&scan, "scan", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	roots := []string{}
	for _, r := range scan {
		abs, err := filepath.Abs(strings.Replace(r, "~", home(), 1))
		if err != nil {
			return fail(err)
		}
		roots = append(roots, abs)
	}
	here, _ := filepath.Abs(c.dir)
	withGlobal, withLocal := true, true
	if c.global != c.local { // exactly one of --global / --local
		withGlobal, withLocal = c.global, c.local
	}
	if c.global && !c.local && len(roots) > 0 {
		return fail(errors.New("--scan reads project folders, so it cannot be combined with --global"))
	}
	if !withLocal {
		here = ""
	}
	if _, _, err := scaffoldRegistry(); err != nil {
		return fail(err)
	}
	reg, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	found, notes, err := scanGlobal(c.agents, withGlobal, here, roots)
	if err != nil {
		return fail(err)
	}
	for _, n := range notes {
		fmt.Println("  !", n)
	}
	var names []string
	skipped := 0
	for n := range found {
		if _, in := reg.Servers[n]; in {
			skipped++
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	if skipped > 0 {
		fmt.Printf("(%d MCP server(s) are already in the mcps list and were skipped)\n", skipped)
	}
	if len(names) == 0 {
		fmt.Println("I found no new MCP servers in your AI tools to add.")
		return 0
	}

	chosen := names
	if !yes {
		if !isTTY() {
			return fail(errors.New("import needs an interactive terminal (or pass --yes to import everything found)"))
		}
		w := 0
		for _, n := range names {
			w = max(w, len(n))
		}
		labels := make([]string, len(names))
		byLabel := map[string]string{}
		for i, n := range names {
			cd := found[n]
			l := fmt.Sprintf("%-*s  [%s]  %s", w, n, cd.agentIDs(), trunc(summary(cd.spec()), 60))
			if cd.folderBound() {
				l += "  (uses files from its own folder)"
			}
			if len(cd.differs) > 0 {
				names := make([]string, len(cd.differs))
				for k, id := range cd.differs {
					names[k] = agentName(id)
				}
				l += "  (different in: " + strings.Join(names, ", ") + ")"
			}
			labels[i], byLabel[l] = l, n
		}
		fmt.Printf("I found %d MCP server(s) already set up in your AI tools (%s).\n"+
			"Choose the ones to add to mcps. Nothing is removed from those tools.\n"+
			"↑/↓ move · space tick/untick · ←/→ none/all · enter confirm · ctrl+c cancel\n",
			len(names), friendlyAgents(found))
		picked, err := pterm.DefaultInteractiveMultiselect.
			WithOptions(labels).
			WithDefaultOptions(labels).
			WithFilter(false).
			WithKeySelect(keys.Space).
			WithKeyConfirm(keys.Enter).
			WithMaxHeight(max(min(len(labels), 15), 3)).
			Show("Add to mcps")
		if err != nil {
			return fail(err)
		}
		chosen = nil
		for _, l := range picked {
			chosen = append(chosen, byLabel[l])
		}
		sort.Strings(chosen)
	}
	if len(chosen) == 0 {
		fmt.Println("Nothing selected, so nothing was added.")
		return 0
	}

	var blocks strings.Builder
	secrets := map[string]string{}
	var warns []string
	for _, n := range chosen {
		d, sec, w := toDef(found[n].spec())
		warns = append(warns, w...)
		if found[n].folderBound() {
			warns = append(warns, n+": uses relative file paths from its original folder; edit its cmd to absolute paths before using it elsewhere")
		}
		for k, v := range sec {
			secrets[k] = v
		}
		blocks.WriteString("\n" + renderServer(n, d))
	}
	cur := readFile(registryPath())
	out := strings.TrimRight(cur, "\n") + "\n" + blocks.String()
	var probe map[string]any
	if _, err := toml.Decode(out, &probe); err != nil {
		return fail(fmt.Errorf("refusing to write: resulting registry would be invalid: %w", err))
	}

	var sv []string
	for k := range secrets {
		sv = append(sv, k)
	}
	sort.Strings(sv)
	if c.dry {
		fmt.Println("dry-run: would append to", tilde(registryPath())+":")
		fmt.Print(blocks.String())
		fmt.Printf("\nsecret values that would move to %s (names only): %s\n", tilde(secretsPath()), orNone(sv))
		return 0
	}
	if err := addSecrets(secrets); err != nil {
		return fail(err)
	}
	if err := writeAtomic(registryPath(), out, true); err != nil {
		return fail(err)
	}
	fmt.Printf("\nAdded %d MCP server(s) to the mcps list (%s).\n", len(chosen), tilde(registryPath()))
	if len(sv) > 0 {
		fmt.Printf("%d API key(s) were stored privately in %s instead of the list.\n", len(sv), tilde(secretsPath()))
	}
	for _, w := range warns {
		fmt.Println("  !", w)
	}
	fmt.Println("Check any command arguments that may contain a key: they are copied as-is.")
	if !guided {
		fmt.Println("Next: run mcps inside a project folder to choose which servers it uses.")
	}
	return 0
}

// friendlyAgents lists the display names of the AI tools the candidates came from.
func friendlyAgents(found map[string]*cand) string {
	set := map[string]bool{}
	for _, c := range found {
		for _, f := range c.from {
			set[f.agent] = true
		}
	}
	var names []string
	for id := range set {
		names = append(names, agentName(id))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
