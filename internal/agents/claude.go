package agents

import "path/filepath"

func init() {
	register(func() Agent {
		h := home()
		return &jsonAgent{
			id:     "claude",
			detect: []string{filepath.Join(h, ".claude.json"), filepath.Join(h, ".claude")},
			glob:   []string{filepath.Join(h, ".claude.json")},
			local:  ".mcp.json",
			key:    "mcpServers",
			entry: func(s *Spec) obj {
				if s.Remote() {
					return s.withHeaders(mk("type", "http", "url", s.URL))
				}
				return stdio(s, true)
			},
		}
	})
}
