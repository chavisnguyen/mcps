package agents

import "path/filepath"

// OpenCode v2 (verified against opencode.ai/docs/mcp-servers and a real v2.3.2
// config): servers live under mcp.servers; local = {type, command[], environment},
// remote = {type, url, headers}. There is no "enabled" field (only "disabled").
func init() {
	register(func() Agent {
		oc := filepath.Join(home(), ".config/opencode")
		return &jsonAgent{
			id:     "opencode",
			detect: []string{oc},
			glob:   []string{filepath.Join(oc, "opencode.jsonc"), filepath.Join(oc, "opencode.json")},
			local:  "opencode.json",
			key:    "mcp",
			sub:    "servers",
			entry: func(s *Spec) obj {
				if s.Remote() {
					return s.withHeaders(mk("type", "remote", "url", s.URL))
				}
				o := mk("type", "local", "command", append([]string{s.Command}, s.Args...))
				if len(s.Env) > 0 {
					o = append(o, kv{"environment", enc(s.Env)})
				}
				return o
			},
		}
	})
}
