package agents

import "path/filepath"

func init() {
	register(func() Agent {
		h := home()
		return &jsonAgent{
			id:     "cursor",
			detect: []string{filepath.Join(h, ".cursor")},
			glob:   []string{filepath.Join(h, ".cursor/mcp.json")},
			local:  ".cursor/mcp.json",
			key:    "mcpServers",
			entry: func(s *Spec) obj {
				if s.Remote() {
					return s.withHeaders(mk("url", s.URL))
				}
				return stdio(s, false)
			},
		}
	})
}
