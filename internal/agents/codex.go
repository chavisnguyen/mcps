package agents

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

func init() { register(func() Agent { return codexAgent{} }) }

type codexAgent struct{}

func (codexAgent) ID() string { return "codex" }
func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	return filepath.Join(home(), ".codex")
}
func (codexAgent) Installed() bool { return exists(codexHome()) }
func (codexAgent) Path(scope, project string) (string, bool) {
	if scope == "local" {
		return filepath.Join(project, ".codex", "config.toml"), true
	}
	return filepath.Join(codexHome(), "config.toml"), true
}

// tableHeader returns the dotted path parts of a "[a.b]" line (not "[[...]]").
func tableHeader(line string) ([]string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "[") || strings.HasPrefix(t, "[[") {
		return nil, false
	}
	end := strings.Index(t, "]")
	if end < 0 {
		return nil, false
	}
	var parts []string
	var cur strings.Builder
	var q rune
	for _, ch := range t[1:end] {
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

func (codexAgent) Names(text string) ([]string, error) {
	seen := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		if p, ok := tableHeader(l); ok && len(p) >= 2 && p[0] == "mcp_servers" {
			seen[p[1]] = true
		}
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return strconv.Quote(k)
}

func (codexAgent) Apply(text, name string, s *Spec) (string, error) {
	var kept []string
	skip := false
	for _, l := range strings.SplitAfter(text, "\n") {
		if p, ok := tableHeader(l); ok {
			skip = len(p) >= 2 && p[0] == "mcp_servers" && p[1] == name
		} else if strings.HasPrefix(strings.TrimSpace(l), "[[") {
			skip = false
		}
		if !skip {
			kept = append(kept, l)
		}
	}
	body := strings.Join(kept, "")
	if strings.TrimSpace(body) == "" {
		body = ""
	} else {
		body = strings.TrimRight(body, " \t\r\n") + "\n"
	}
	if s == nil {
		return body, nil
	}
	k := tomlKey(name)
	var b strings.Builder
	b.WriteString("[mcp_servers." + k + "]\n")
	if s.Remote() {
		b.WriteString("url = " + string(enc(s.URL)) + "\n")
		if len(s.Headers) > 0 {
			keys := make([]string, 0, len(s.Headers))
			for h := range s.Headers {
				keys = append(keys, h)
			}
			sort.Strings(keys)
			parts := make([]string, len(keys))
			for i, h := range keys {
				parts[i] = tomlKey(h) + " = " + string(enc(s.Headers[h]))
			}
			b.WriteString("http_headers = { " + strings.Join(parts, ", ") + " }\n")
		}
	} else {
		b.WriteString("command = " + string(enc(s.Command)) + "\n")
		if len(s.Args) > 0 {
			b.WriteString("args = " + string(enc(s.Args)) + "\n")
		}
		if len(s.Env) > 0 {
			b.WriteString("\n[mcp_servers." + k + ".env]\n")
			keys := make([]string, 0, len(s.Env))
			for e := range s.Env {
				keys = append(keys, e)
			}
			sort.Strings(keys)
			for _, e := range keys {
				b.WriteString(tomlKey(e) + " = " + string(enc(s.Env[e])) + "\n")
			}
		}
	}
	out := b.String()
	if body != "" {
		out = body + "\n" + out
	}
	var check map[string]any
	if _, err := toml.Decode(out, &check); err != nil {
		return "", errors.New("generated TOML is invalid: " + err.Error())
	}
	return out, nil
}

// Read parses [mcp_servers.*] tables back into Specs.
func (codexAgent) Read(text string) ([]*Spec, error) {
	var doc struct {
		Servers map[string]map[string]any `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, err
	}
	var out []*Spec
	for name, m := range doc.Servers {
		s := &Spec{Name: name}
		if u, ok := m["url"].(string); ok && u != "" {
			s.URL = u
			s.Headers = strMap(m["http_headers"])
			if v, ok := m["bearer_token_env_var"].(string); ok && v != "" {
				if s.Headers == nil {
					s.Headers = map[string]string{}
				}
				s.Headers["Authorization"] = "Bearer ${" + v + "}"
			}
			for h, v := range strMap(m["env_http_headers"]) {
				if s.Headers == nil {
					s.Headers = map[string]string{}
				}
				s.Headers[h] = "${" + v + "}"
			}
		} else if cmd, ok := m["command"].(string); ok && cmd != "" {
			s.Command, s.Args, s.Env = cmd, strList(m["args"]), strMap(m["env"])
		} else {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
