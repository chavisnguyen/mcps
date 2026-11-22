package agents

import (
	"strings"
	"testing"
)

func find(t *testing.T, id string) Agent {
	t.Helper()
	for _, a := range All() {
		if a.ID() == id {
			return a
		}
	}
	t.Fatalf("no agent %q", id)
	return nil
}

// Every agent must read back exactly what it wrote, for remote (with headers) and stdio (with env).
func TestRoundTripAllAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	remote := &Spec{Name: "r", URL: "https://x/mcp", Headers: map[string]string{"Authorization": "Bearer k"}}
	local := &Spec{Name: "l", Command: "npx", Args: []string{"-y", "pkg"}, Env: map[string]string{"TOKEN": "v"}}
	for _, a := range All() {
		if a.ID() == "desktop" {
			continue // remote is bridged through mcp-remote; covered in import_test
		}
		text, err := a.Apply("", "r", remote)
		if err == nil {
			text, err = a.Apply(text, "l", local)
		}
		if err != nil {
			t.Fatalf("%s: %v", a.ID(), err)
		}
		got, err := a.Read(text)
		if err != nil || len(got) != 2 {
			t.Fatalf("%s: read %v %v\n%s", a.ID(), got, err, text)
		}
		m := map[string]*Spec{got[0].Name: got[0], got[1].Name: got[1]}
		if m["r"].URL != remote.URL || m["r"].Headers["Authorization"] != "Bearer k" {
			t.Errorf("%s: remote lost data: %+v", a.ID(), m["r"])
		}
		if m["l"].Command != "npx" || len(m["l"].Args) != 2 || m["l"].Env["TOKEN"] != "v" {
			t.Errorf("%s: local lost data: %+v", a.ID(), m["l"])
		}
		text, _ = a.Apply(text, "r", nil)
		if n, _ := a.Names(text); len(n) != 1 || n[0] != "l" {
			t.Errorf("%s: remove failed: %v", a.ID(), n)
		}
	}
}

// OpenCode v2 shape, verified against opencode.ai docs and a real v2.3.2 config.
func TestOpenCodeV2Shape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := find(t, "opencode")
	text, err := a.Apply(`{"theme":"x","mcp":{"servers":{"old":{"type":"remote","url":"http://o"}},"other":1}}`, "n",
		&Spec{Name: "n", Command: "npx", Args: []string{"-y", "p"}, Env: map[string]string{"K": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"servers"`, `"old"`, `"other"`, `"theme"`, `"type": "local"`,
		`"command": [`, `"environment"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "enabled") {
		t.Errorf("v2 has no enabled field:\n%s", text)
	}
	// removing the last server keeps sibling keys
	text, _ = a.Apply(text, "n", nil)
	text, _ = a.Apply(text, "old", nil)
	if !strings.Contains(text, `"other"`) || strings.Contains(text, `"servers"`) {
		t.Errorf("cleanup wrong:\n%s", text)
	}
}
