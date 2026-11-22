package agents

import "path/filepath"

// AGY (Antigravity): global ~/.gemini/config/mcp_config.json, project .agents/mcp_config.json.
func init() {
	register(func() Agent {
		h := home()
		return &jsonAgent{
			id:     "agy",
			detect: []string{filepath.Join(h, ".gemini")},
			glob:   []string{filepath.Join(h, ".gemini/config/mcp_config.json")},
			local:  ".agents/mcp_config.json",
			key:    "mcpServers",
			entry: func(s *Spec) obj {
				if s.Remote() {
					return s.withHeaders(mk("serverUrl", s.URL, "disabled", false))
				}
				return stdio(s, false)
			},
		}
	})
}
