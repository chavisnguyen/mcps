package agents

import (
	"path/filepath"
	"sort"
	"strings"
)

// Claude Desktop only speaks stdio: remote servers are bridged with mcp-remote.
// Global only.
func init() {
	register(func() Agent {
		app := filepath.Join(home(), "Library/Application Support/Claude")
		return &desktopAgent{&jsonAgent{
			id:     "desktop",
			detect: []string{app},
			glob:   []string{filepath.Join(app, "claude_desktop_config.json")},
			key:    "mcpServers",
			entry: func(s *Spec) obj {
				if s.Remote() {
					a := []string{"-y", "mcp-remote", s.URL}
					keys := make([]string, 0, len(s.Headers))
					for k := range s.Headers {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						a = append(a, "--header", k+": "+s.Headers[k])
					}
					return mk("command", "npx", "args", a)
				}
				return stdio(s, false)
			},
		}}
	})
}

type desktopAgent struct{ *jsonAgent }

// Read maps entries written as `npx -y mcp-remote URL [--header "K: V"]` back to
// the remote server they represent, so they are not mistaken for a local command.
func (d *desktopAgent) Read(text string) ([]*Spec, error) {
	specs, err := d.jsonAgent.Read(text)
	if err != nil {
		return nil, err
	}
	for _, s := range specs {
		i := -1
		for k, a := range s.Args {
			if strings.Contains(a, "mcp-remote") {
				i = k
				break
			}
		}
		if i < 0 {
			continue
		}
		url := ""
		hdr := map[string]string{}
		for k := i + 1; k < len(s.Args); k++ {
			a := s.Args[k]
			switch {
			case a == "--header" && k+1 < len(s.Args):
				if n, v, ok := strings.Cut(s.Args[k+1], ":"); ok {
					hdr[strings.TrimSpace(n)] = strings.TrimSpace(v)
				}
				k++
			case url == "" && strings.HasPrefix(a, "http"):
				url = a
			}
		}
		if url != "" {
			s.URL, s.Command, s.Args, s.Env = url, "", nil, nil
			if len(hdr) > 0 {
				s.Headers = hdr
			}
		}
	}
	return specs, nil
}
