package agents

import "path/filepath"

// NOTE: schema (mcp.<name> with type local|remote) follows OpenCode's documented
// format as I know it; verify against your installed version.
func init() {
	register(func() Agent {
		oc := filepath.Join(home(), ".config/opencode")
		return &jsonAgent{
			id:     "opencode",
			detect: []string{oc},
			glob:   []string{filepath.Join(oc, "opencode.jsonc"), filepath.Join(oc, "opencode.json")},
			local:  "opencode.json",
			key:    "mcp",
			entry: func(s *Spec) obj {
				if s.Remote() {
					return s.withHeaders(mk("type", "remote", "url", s.URL, "enabled", true))
				}
				o := mk("type", "local", "command", append([]string{s.Command}, s.Args...), "enabled", true)
				if len(s.Env) > 0 {
					o = append(o, kv{"environment", enc(s.Env)})
				}
				return o
			},
		}
	})
}
