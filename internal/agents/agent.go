// Package agents knows where each AI tool keeps its MCP config and how to edit it.
// One file per agent; each registers itself in init(). To add an agent: copy a
// small one (cursor.go), change paths and entry shape, done.
package agents

import (
	"os"
	"regexp"
	"sort"
)

var trailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// Spec is one fully-resolved MCP server (secrets already expanded).
type Spec struct {
	Name    string
	URL     string
	Command string
	Args    []string
	Env     map[string]string
	Headers map[string]string // remote servers only (auth etc.)
}

func (s *Spec) Remote() bool { return s.URL != "" }

func (s *Spec) args() []string {
	if s.Args == nil {
		return []string{}
	}
	return s.Args
}

// withHeaders appends "headers" to a remote entry when the spec has any.
func (s *Spec) withHeaders(o obj) obj {
	if len(s.Headers) > 0 {
		o = append(o, kv{"headers", enc(s.Headers)})
	}
	return o
}

// Agent edits one tool's MCP config.
type Agent interface {
	ID() string
	Installed() bool
	// Path returns the config file for the scope ("global"|"local"); ok=false if
	// the agent has no such scope.
	Path(scope, project string) (path string, ok bool)
	// Apply returns new file text with server `name` set (s != nil) or removed.
	Apply(text, name string, s *Spec) (string, error)
	Names(text string) ([]string, error)
	// Read parses every server in the file back into Specs (used by import).
	Read(text string) ([]*Spec, error)
}

// factories (not instances): paths are resolved at call time so $HOME changes
// (tests) are honoured.
var factories []func() Agent

func register(f func() Agent) { factories = append(factories, f) }

// All returns every known agent, sorted by ID.
func All() []Agent {
	out := make([]Agent, len(factories))
	for i, f := range factories {
		out[i] = f()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

func home() string { h, _ := os.UserHomeDir(); return h }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// stdio builds the common {command,args,env} entry; typ adds "type":"stdio".
func stdio(s *Spec, typ bool) obj {
	o := obj{}
	if typ {
		o = append(o, kv{"type", enc("stdio")})
	}
	o = append(o, kv{"command", enc(s.Command)}, kv{"args", enc(s.args())})
	if len(s.Env) > 0 {
		o = append(o, kv{"env", enc(s.Env)})
	}
	return o
}
