package agents

import (
	"encoding/json"
	"errors"
	"sort"
)

// jsonAgent is the shared base for agents whose config is JSON with servers
// under top[key][name]. Concrete agents only supply paths and entry().
type jsonAgent struct {
	id     string
	detect []string
	glob   []string // candidate global files; first existing wins, else the last
	local  string   // project-relative path; "" = no project-level config
	key    string
	entry  func(*Spec) obj
}

func (a *jsonAgent) ID() string { return a.id }

func (a *jsonAgent) Installed() bool {
	for _, p := range a.detect {
		if exists(p) {
			return true
		}
	}
	return false
}

func (a *jsonAgent) Path(scope, project string) (string, bool) {
	if scope == "local" {
		if a.local == "" {
			return "", false
		}
		return project + "/" + a.local, true
	}
	for _, p := range a.glob {
		if exists(p) {
			return p, true
		}
	}
	return a.glob[len(a.glob)-1], true
}

func (a *jsonAgent) load(text string) (top, servers obj, err error) {
	clean, comments := stripJSONC(text)
	if comments {
		return nil, nil, errors.New("file has comments; refusing to rewrite (they would be lost)")
	}
	if top, err = parseObj(clean); err != nil {
		return nil, nil, err
	}
	servers = obj{}
	if i := top.idx(a.key); i >= 0 {
		if servers, err = parseObj(string(top[i].V)); err != nil {
			return nil, nil, err
		}
	}
	return top, servers, nil
}

func (a *jsonAgent) Apply(text, name string, s *Spec) (string, error) {
	top, servers, err := a.load(text)
	if err != nil {
		return "", err
	}
	if s == nil {
		servers.del(name)
		if len(servers) == 0 {
			top.del(a.key)
		} else {
			top.set(a.key, enc(servers))
		}
	} else {
		servers.set(name, enc(a.entry(s)))
		top.set(a.key, enc(servers))
	}
	return top.pretty(), nil
}

func (a *jsonAgent) Names(text string) ([]string, error) {
	_, servers, err := a.load(text)
	if err != nil {
		return nil, err
	}
	ks := servers.keys()
	sort.Strings(ks)
	return ks, nil
}

// Read parses all servers in the file (comments are fine for reading).
func (a *jsonAgent) Read(text string) ([]*Spec, error) {
	clean, _ := stripJSONC(text)
	top, err := parseObj(clean)
	if err != nil {
		return nil, err
	}
	i := top.idx(a.key)
	if i < 0 {
		return nil, nil
	}
	servers, err := parseObj(string(top[i].V))
	if err != nil {
		return nil, err
	}
	var out []*Spec
	for _, e := range servers {
		if s := specFromJSON(e.K, e.V); s != nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func strMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, x := range m {
		if s, ok := x.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func strList(v any) []string {
	l, _ := v.([]any)
	var out []string
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// specFromJSON understands every agent's entry shape (url/serverUrl/httpUrl,
// command string or OpenCode's command array, env/environment, headers).
func specFromJSON(name string, raw json.RawMessage) *Spec {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	s := &Spec{Name: name}
	for _, k := range []string{"url", "serverUrl", "httpUrl"} {
		if u, ok := m[k].(string); ok && u != "" {
			s.URL = u
			break
		}
	}
	if s.URL != "" {
		s.Headers = strMap(m["headers"])
		return s
	}
	switch c := m["command"].(type) {
	case string:
		s.Command, s.Args = c, strList(m["args"])
	case []any:
		l := strList(c)
		if len(l) > 0 {
			s.Command, s.Args = l[0], l[1:]
		}
	}
	if s.Command == "" {
		return nil
	}
	if e := strMap(m["env"]); e != nil {
		s.Env = e
	} else {
		s.Env = strMap(m["environment"])
	}
	return s
}
