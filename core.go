package main

import (
	"encoding/json"
	"fmt"
	"github.com/chavisnguyen/mcps/internal/agents"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ------------------------------------------------------------------- paths

func hubHome() string {
	if h := os.Getenv("MCPS_HOME"); h != "" {
		return h
	}
	return filepath.Join(home(), ".mcps")
}

func configDir() string { return filepath.Join(hubHome(), "config") }

func registryPath() string {
	if p := os.Getenv("MCPS_REGISTRY"); p != "" {
		return p
	}
	return filepath.Join(configDir(), "servers.toml")
}

var backupStamp = time.Now().Format("20060102-150405")

// backupRoot is resolved lazily so MCPS_HOME/HOME set after process start
// (e.g. by tests) are honoured.
func backupRoot() string { return filepath.Join(hubHome(), "backups", backupStamp) }

// ---------------------------------------------------------------- registry

type ServerDef struct {
	URL     string            `toml:"url,omitempty"`
	Cmd     []string          `toml:"cmd,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
	Headers map[string]string `toml:"headers,omitempty"`
	Install string            `toml:"install,omitempty"`
}

type Registry struct {
	Global struct {
		Use []string `toml:"use"`
	} `toml:"global"`
	Servers map[string]ServerDef `toml:"servers"`
}

// strictKeys rejects keys the schema does not know (typos like "instal").
func strictKeys(md toml.MetaData) error {
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return fmt.Errorf("unknown key(s): %s (typo?)", strings.Join(keys, ", "))
	}
	return nil
}

type Project struct {
	Use    []string `toml:"use"`
	Agents []string `toml:"agents,omitempty"`
}

func loadRegistry() (*Registry, error) {
	p := registryPath()
	var r Registry
	md, err := toml.DecodeFile(p, &r)
	if err == nil {
		err = strictKeys(md)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no registry at %s (run `mcps init` to create one, or set MCPS_REGISTRY)", p)
		}
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	for n, d := range r.Servers {
		if d.URL == "" && len(d.Headers) > 0 {
			return nil, fmt.Errorf("server %q: headers only apply to url servers", n)
		}
		if (d.URL == "") == (len(d.Cmd) == 0) {
			return nil, fmt.Errorf("server %q: set exactly one of url or cmd", n)
		}
	}
	return &r, nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolve turns registry entries into Specs, expanding ${VAR} from the
// environment. Missing variables abort before anything is written.
func (r *Registry) resolve(names []string) ([]*agents.Spec, error) {
	var specs []*agents.Spec
	var missing []string
	expand := func(s string) string {
		return envRef.ReplaceAllStringFunc(s, func(m string) string {
			v := envRef.FindStringSubmatch(m)[1]
			val, ok := lookupVar(v)
			if !ok {
				missing = append(missing, v)
			}
			return val
		})
	}
	for _, n := range names {
		d, ok := r.Servers[n]
		if !ok {
			return nil, fmt.Errorf("server %q is not defined in the registry", n)
		}
		s := &agents.Spec{Name: n, URL: expand(d.URL)}
		if len(d.Cmd) > 0 {
			s.Command = expand(d.Cmd[0])
			for _, a := range d.Cmd[1:] {
				s.Args = append(s.Args, expand(a))
			}
		}
		if len(d.Headers) > 0 {
			s.Headers = map[string]string{}
			for k, v := range d.Headers {
				s.Headers[k] = expand(v)
			}
		}
		if len(d.Env) > 0 {
			s.Env = map[string]string{}
			for k, v := range d.Env {
				s.Env[k] = expand(v)
			}
		}
		specs = append(specs, s)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("missing value for: %s\n  set it as an environment variable, or add a NAME=value line to %s",
			strings.Join(uniq(missing), ", "), tilde(secretsPath()))
	}
	return specs, nil
}

func uniq(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// ------------------------------------------------------------------- state
// state remembers which server names mcps manages in which file, so sync
// only ever removes entries it created itself.

type state struct {
	Files map[string][]string `json:"files"`
}

func statePath() string { return filepath.Join(hubHome(), "state.json") }

func loadState() *state {
	s := &state{Files: map[string][]string{}}
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, s)
		if s.Files == nil {
			s.Files = map[string][]string{}
		}
	}
	return s
}

func (s *state) save() error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return writeAtomic(statePath(), string(b)+"\n", false)
}

// ------------------------------------------------------------------- files

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func writeAtomic(path, text string, backup bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		if backup {
			dst := filepath.Join(backupRoot(), strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "__"))
			if err := os.MkdirAll(backupRoot(), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(dst, []byte(readFile(path)), 0o600); err != nil {
				return err
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcps-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// miniDiff prints the changed region with 2 lines of context.
func miniDiff(old, new string) string {
	a, b := strings.Split(old, "\n"), strings.Split(new, "\n")
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	var out strings.Builder
	from := max(p-2, 0)
	for _, l := range a[from:p] {
		out.WriteString("      " + l + "\n")
	}
	for _, l := range a[p : len(a)-s] {
		out.WriteString("    - " + l + "\n")
	}
	for _, l := range b[p : len(b)-s] {
		out.WriteString("    + " + l + "\n")
	}
	to := min(len(a)-s+2, len(a))
	for _, l := range a[len(a)-s : to] {
		out.WriteString("      " + l + "\n")
	}
	return out.String()
}

// -------------------------------------------------------------- apply engine

type op struct {
	name string
	spec *agents.Spec // nil = remove
}

type result struct{ agent, path, status, detail string }

type target struct {
	quiet   bool      // collect results instead of printing per-agent lines
	res     *[]result // where to collect (optional)
	scope   string    // global | local
	project string
	agents  []agents.Agent
	dry     bool
	replace bool // remove every entry not in desired (not just ones mcps created)
}

// pickAgents resolves --agents, else every agent detected on this machine.
func pickAgents(csv string) ([]agents.Agent, bool, error) {
	all := agents.All()
	if csv == "" {
		var out []agents.Agent
		for _, a := range all {
			if a.Installed() {
				out = append(out, a)
			}
		}
		return out, false, nil
	}
	var out []agents.Agent
	for _, id := range strings.Split(csv, ",") {
		id = strings.TrimSpace(id)
		var found agents.Agent
		for _, a := range all {
			if a.ID() == id {
				found = a
			}
		}
		if found == nil {
			ids := make([]string, len(all))
			for i, a := range all {
				ids[i] = a.ID()
			}
			return nil, true, fmt.Errorf("unknown agent %q (known: %s)", id, strings.Join(ids, ", "))
		}
		out = append(out, found)
	}
	return out, true, nil
}

// apply runs the same ops on every agent. managed (optional) is the state to
// track: when non-nil, entries previously managed but absent from desired are
// removed, and state is updated for each written file.
func apply(t target, desired []*agents.Spec, extraOps []op, track bool) (failures int) {
	st := loadState()
	say := func(f string, a ...any) {
		if !t.quiet {
			fmt.Printf(f, a...)
		}
	}
	rec := func(a agents.Agent, path, status, detail string) {
		if t.res != nil {
			*t.res = append(*t.res, result{a.ID(), path, status, detail})
		}
	}
	desiredNames := map[string]bool{}
	for _, s := range desired {
		desiredNames[s.Name] = true
	}
	for _, a := range t.agents {
		path, ok := a.Path(t.scope, t.project)
		if !ok {
			say("  - %-9s skip (no project-level config for this agent)\n", a.ID())
			rec(a, "", "skipped", "")
			continue
		}
		old := readFile(path)
		cur, err := a.Names(old)
		if err != nil {
			failures++
			say("  ! %-9s ERROR %v [%s]\n", a.ID(), err, path)
			rec(a, path, "error", err.Error())
			continue
		}
		have := map[string]bool{}
		for _, n := range cur {
			have[n] = true
		}
		var ops []op
		var notes []string
		prev := map[string]bool{}
		if track {
			for _, n := range st.Files[path] {
				prev[n] = true
			}
			if t.replace {
				for _, n := range cur {
					prev[n] = true
				}
			}
			for n := range prev {
				if !desiredNames[n] && have[n] {
					ops = append(ops, op{n, nil})
					notes = append(notes, "removed "+n)
				}
			}
			for _, s := range desired {
				if have[s.Name] && !prev[s.Name] {
					notes = append(notes, "took over existing "+s.Name)
				}
				ops = append(ops, op{s.Name, s})
			}
		}
		ops = append(ops, extraOps...)
		sort.SliceStable(ops, func(i, j int) bool { return ops[i].spec == nil && ops[j].spec != nil })

		text := old
		var opErr error
		for _, o := range ops {
			if o.spec == nil && !have[o.name] {
				continue
			}
			if text, opErr = a.Apply(text, o.name, o.spec); opErr != nil {
				break
			}
		}
		if opErr != nil {
			failures++
			say("  ! %-9s ERROR %v [%s]\n", a.ID(), opErr, path)
			rec(a, path, "error", opErr.Error())
			continue
		}
		extra := ""
		if len(notes) > 0 {
			sort.Strings(notes)
			extra = "  (" + strings.Join(notes, "; ") + ")"
		}
		if text == old && old == "" {
			say("  - %-9s nothing to write (no servers, no file)\n", a.ID())
			rec(a, path, "unchanged", "")
		} else if text == old {
			say("  = %-9s unchanged    %s\n", a.ID(), path)
			rec(a, path, "unchanged", "")
		} else if t.dry {
			say("  ~ %-9s would change %s%s\n%s", a.ID(), path, extra, miniDiff(old, text))
			rec(a, path, "would-change", extra)
		} else {
			if err := writeAtomic(path, text, true); err != nil {
				failures++
				say("  ! %-9s ERROR %v\n", a.ID(), err)
				rec(a, path, "error", err.Error())
				continue
			}
			say("  + %-9s written      %s%s\n", a.ID(), path, extra)
			rec(a, path, "written", extra)
		}
		if track && !t.dry {
			var names []string
			for n := range desiredNames {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) == 0 {
				delete(st.Files, path)
			} else {
				st.Files[path] = names
			}
		}
	}
	if track && !t.dry {
		if err := st.save(); err != nil {
			say("  ! could not save state: %v\n", err)
			failures++
		}
	}
	return failures
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func home() string { h, _ := os.UserHomeDir(); return h }

// ------------------------------------------------------------------ secrets
// ${VAR} resolves from the process environment first, then from
// ~/.mcps/secrets.env (KEY=value lines, mode 0600) so GUI-launched shells and
// imported values work without exporting anything.

func secretsPath() string { return filepath.Join(hubHome(), "secrets.env") }

var secretsCache map[string]string

func loadSecrets() map[string]string {
	if secretsCache != nil {
		return secretsCache
	}
	secretsCache = map[string]string{}
	for _, l := range strings.Split(readFile(secretsPath()), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if u, err := strconv.Unquote(v); err == nil {
			v = u
		}
		secretsCache[strings.TrimSpace(k)] = v
	}
	return secretsCache
}

func lookupVar(name string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	v, ok := loadSecrets()[name]
	return v, ok
}

// addSecrets merges new KEY=value pairs (existing keys are kept) with mode 0600.
func addSecrets(add map[string]string) error {
	if len(add) == 0 {
		return nil
	}
	cur := loadSecrets()
	var b strings.Builder
	b.WriteString("# mcps secrets: KEY=value, referenced as ${KEY} in servers.toml. Keep private (0600).\n")
	keys := map[string]string{}
	for k, v := range cur {
		keys[k] = v
	}
	for k, v := range add {
		if _, ok := keys[k]; !ok {
			keys[k] = v
		}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		b.WriteString(k + "=" + strconv.Quote(keys[k]) + "\n")
	}
	if err := writeAtomic(secretsPath(), b.String(), false); err != nil {
		return err
	}
	secretsCache = nil
	return os.Chmod(secretsPath(), 0o600)
}

// tilde shortens paths under the home directory to ~/... for display.
func tilde(p string) string {
	if h := home(); h != "" && (p == h || strings.HasPrefix(p, h+string(os.PathSeparator))) {
		return "~" + p[len(h):]
	}
	return p
}
