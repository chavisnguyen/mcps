package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncAddsRemovesOnlyManaged(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	t.Setenv("TOK", "s3cret")
	os.MkdirAll(filepath.Join(h, ".kiro"), 0o755)
	reg := filepath.Join(h, "servers.toml")
	os.WriteFile(reg, []byte(`
[servers.a]
url = "http://a"
[servers.b]
cmd = ["x", "y"]
env = { T = "${TOK}" }
`), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	kiro := filepath.Join(h, ".kiro/settings/mcp.json")
	os.MkdirAll(filepath.Dir(kiro), 0o755)
	os.WriteFile(kiro, []byte(`{"z":1,"mcpServers":{"hand":{"url":"http://h"}}}`), 0o644)

	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use = ["a"]`), 0o644)
	// kiro has no project scope here? it does: .kiro/settings/mcp.json in project
	if run([]string{"sync", "--dir", proj, "--agents", "kiro"}) != 0 {
		t.Fatal("sync failed")
	}
	pf := filepath.Join(proj, ".kiro/settings/mcp.json")
	if !strings.Contains(readFile(pf), `"a"`) {
		t.Fatalf("a missing: %s", readFile(pf))
	}
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use = ["b"]`), 0o644)
	if run([]string{"sync", "--dir", proj, "--agents", "kiro"}) != 0 {
		t.Fatal("sync 2 failed")
	}
	got := readFile(pf)
	if strings.Contains(got, `"a"`) || !strings.Contains(got, "s3cret") {
		t.Fatalf("expected a removed and b added with secret: %s", got)
	}
	// global file with a hand-made entry must be untouched by project sync
	if !strings.Contains(readFile(kiro), `"hand"`) {
		t.Fatal("hand entry lost")
	}
}

func TestMissingEnvAbortsBeforeWrite(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	reg := filepath.Join(h, "servers.toml")
	os.WriteFile(reg, []byte("[servers.b]\ncmd=[\"x\"]\nenv={T=\"${NOPE_X}\"}\n"), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use=["b"]`), 0o644)
	if run([]string{"sync", "--dir", proj, "--agents", "claude"}) == 0 {
		t.Fatal("expected failure")
	}
	if exists(filepath.Join(proj, ".mcp.json")) {
		t.Fatal("must not write when env missing")
	}
}

func TestProjectAgentsLimitsOutput(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	reg := filepath.Join(h, "servers.toml")
	os.WriteFile(reg, []byte("[servers.a]\nurl=\"http://a\"\n"), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte("use=[\"a\"]\nagents=[\"claude\"]\n"), 0o644)
	if run([]string{"sync", "--dir", proj}) != 0 {
		t.Fatal("sync failed")
	}
	if !exists(filepath.Join(proj, ".mcp.json")) || exists(filepath.Join(proj, ".codex/config.toml")) {
		t.Fatal("agents = [claude] must produce only .mcp.json")
	}
}
