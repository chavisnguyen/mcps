package main

import (
	"flag"
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/chavisnguyen/mcps/internal/agents"
)

// -e K=V stores V; -e K (no value) stores a ${K} reference.
type refFlag map[string]string

func (r refFlag) String() string { return "" }
func (r refFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		val = "${" + k + "}"
	}
	r[k] = val
	return nil
}

// -H "Name: value"
type headerFlag map[string]string

func (h headerFlag) String() string { return "" }
func (h headerFlag) Set(v string) error {
	n, val, ok := strings.Cut(v, ":")
	if !ok || strings.TrimSpace(n) == "" {
		return fmt.Errorf(`-H wants "Name: value"`)
	}
	h[strings.TrimSpace(n)] = strings.TrimSpace(val)
	return nil
}

// cmdAdd adds a server to the mcps list (servers.toml).
func cmdAdd(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fail(fmt.Errorf("usage: mcps add NAME --url URL   or   mcps add NAME [-e KEY[=VALUE]] -- COMMAND ARGS..."))
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
	fs := newFlags("add", &c, nil, &url)
	env, hdr := refFlag{}, headerFlag{}
	fs.Var(env, "e", "")
	fs.Var(hdr, "H", "")
	fs.Var(hdr, "header", "")
	if err := fs.Parse(flags); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if (url == "") == (len(cmdline) == 0) {
		return fail(fmt.Errorf("give either --url URL, or a command after `--`\n  e.g. mcps add github -- npx -y @modelcontextprotocol/server-github"))
	}
	if url != "" && len(env) > 0 {
		return fail(fmt.Errorf("-e only applies to local servers; use -H \"Name: value\" for a remote server's headers"))
	}
	if url == "" && len(hdr) > 0 {
		return fail(fmt.Errorf("-H only applies to remote servers (--url)"))
	}
	if _, _, err := scaffoldRegistry(); err != nil {
		return fail(err)
	}
	reg, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	if _, dup := reg.Servers[name]; dup {
		return fail(fmt.Errorf("%q is already in the mcps list. Remove it first: mcps remove %s", name, name))
	}
	s := &agents.Spec{Name: name, URL: url}
	if url == "" {
		s.Command, s.Args = cmdline[0], cmdline[1:]
		if len(env) > 0 {
			s.Env = env
		}
	} else if len(hdr) > 0 {
		s.Headers = hdr
	}
	def, secrets, warns := toDef(s)
	out := strings.TrimRight(readFile(registryPath()), "\n") + "\n\n" + renderServer(name, def)
	var probe map[string]any
	if _, err := toml.Decode(out, &probe); err != nil {
		return fail(fmt.Errorf("refusing to write: the list would become invalid: %w", err))
	}
	if err := addSecrets(secrets); err != nil {
		return fail(err)
	}
	if err := writeAtomic(registryPath(), out, true); err != nil {
		return fail(err)
	}
	fmt.Printf("Added %q to the mcps list.\n  %s\n", name, summary(s))
	if len(secrets) > 0 {
		fmt.Printf("Your key is stored privately in %s, not in the list.\n", tilde(secretsPath()))
	}
	for _, w := range warns {
		fmt.Println("!", w)
	}
	for k, v := range env {
		if v == "${"+k+"}" {
			fmt.Printf("Note: ${%s} is read from your environment (or %s).\n", k, tilde(secretsPath()))
		}
	}
	fmt.Println("Next: run mcps inside a project folder and tick it.")
	return 0
}

// cmdRemove deletes a server from the mcps list.
func cmdRemove(args []string) int {
	if len(args) != 1 {
		return fail(fmt.Errorf("usage: mcps remove NAME"))
	}
	name := args[0]
	reg, err := loadRegistry()
	if err != nil {
		return fail(err)
	}
	if _, ok := reg.Servers[name]; !ok {
		return fail(fmt.Errorf("%q is not in the mcps list (see: mcps list)", name))
	}
	out := removeSection(readFile(registryPath()), name)
	var probe map[string]any
	if _, err := toml.Decode(out, &probe); err != nil {
		return fail(fmt.Errorf("refusing to write: the list would become invalid: %w", err))
	}
	if err := writeAtomic(registryPath(), out, true); err != nil {
		return fail(err)
	}
	fmt.Printf("Removed %q from the mcps list.\n", name)
	fmt.Println("Folders that use it keep their current files; it is dropped the next time you run mcps there.")
	return 0
}

var tableLine = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)

func headerParts(line string) ([]string, bool) {
	m := tableLine.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	var parts []string
	var cur strings.Builder
	var q rune
	for _, ch := range m[1] {
		switch {
		case ch == '"' || ch == '\'':
			if q == 0 {
				q = ch
			} else if q == ch {
				q = 0
			}
		case ch == '.' && q == 0:
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	return append(parts, strings.TrimSpace(cur.String())), true
}

// removeSection drops [servers.NAME] and its sub-tables, keeping everything else.
func removeSection(text, name string) string {
	var kept []string
	skip := false
	for _, l := range strings.SplitAfter(text, "\n") {
		if p, ok := headerParts(l); ok {
			skip = len(p) >= 2 && p[0] == "servers" && p[1] == name
		} else if strings.HasPrefix(strings.TrimSpace(l), "[[") {
			skip = false
		}
		if !skip {
			kept = append(kept, l)
		}
	}
	out := strings.Join(kept, "")
	return strings.TrimRight(out, " \t\r\n") + "\n"
}
