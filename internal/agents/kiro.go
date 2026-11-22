package agents

import "path/filepath"

func init() {
	register(func() Agent {
		h := home()
		return &jsonAgent{
			id:     "kiro",
			detect: []string{filepath.Join(h, ".kiro")},
			glob:   []string{filepath.Join(h, ".kiro/settings/mcp.json")},
			local:  ".kiro/settings/mcp.json",
			key:    "mcpServers",
			entry: func(s *Spec) obj {
				o := stdio(s, false)
				if s.Remote() {
					o = s.withHeaders(mk("url", s.URL))
				}
				return append(o, kv{"disabled", enc(false)})
			},
		}
	})
}
