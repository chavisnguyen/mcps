package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportScansGlobalAndKeepsSecretsOutOfRegistry(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	t.Setenv("MCPS_REGISTRY", "")
	os.MkdirAll(filepath.Join(h, ".codex"), 0o755)
	os.MkdirAll(filepath.Join(h, ".gemini/config"), 0o755)
	os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{
	  "numStartups": 3,
	  "mcpServers": {
	    "gh":   {"type":"stdio","command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"ghp_REALSECRET","LOG":"debug"}},
	    "slack":{"type":"http","url":"http://localhost:3010/mcp"}
	  }}`), 0o644)
	os.WriteFile(filepath.Join(h, ".gemini/config/mcp_config.json"), []byte(`{"mcpServers":{
	    "composio":{"serverUrl":"https://c.example/mcp","headers":{"x-consumer-api-key":"KEY123"},"disabled":false},
	    "slack":{"serverUrl":"http://localhost:3010/mcp"}}}`), 0o644)
	os.WriteFile(filepath.Join(h, ".codex/config.toml"), []byte(`
[mcp_servers.fimake]
url = "http://localhost:10101/mcp"
`), 0o644)

	if code := run([]string{"import", "--yes"}); code != 0 {
		t.Fatalf("import failed: %d", code)
	}
	reg := readFile(registryPath())
	for _, secret := range []string{"ghp_REALSECRET", "KEY123"} {
		if strings.Contains(reg, secret) {
			t.Fatalf("secret %q leaked into registry:\n%s", secret, reg)
		}
	}
	for _, want := range []string{"[servers.gh]", "[servers.composio]", "[servers.fimake]", "[servers.slack]",
		"${GH_GITHUB_TOKEN}", "${COMPOSIO_X_CONSUMER_API_KEY}", `LOG = "debug"`} {
		if !strings.Contains(reg, want) {
			t.Fatalf("registry missing %q:\n%s", want, reg)
		}
	}
	if strings.Count(reg, "[servers.slack]") != 1 {
		t.Fatalf("slack (same in two agents) must be imported once:\n%s", reg)
	}
	fi, err := os.Stat(secretsPath())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.env must exist with mode 0600: %v %v", err, fi)
	}
	// re-import: everything already known
	if code := run([]string{"import", "--yes"}); code != 0 {
		t.Fatal("second import failed")
	}
	if strings.Count(readFile(registryPath()), "[servers.gh]") != 1 {
		t.Fatal("second import must not duplicate")
	}

	// end to end: a project uses gh, sync resolves the secret from secrets.env
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use=["gh"]`), 0o644)
	os.Unsetenv("GH_GITHUB_TOKEN")
	if code := run([]string{"sync", "--dir", proj, "--agents", "claude"}); code != 0 {
		t.Fatal("sync failed")
	}
	if !strings.Contains(readFile(filepath.Join(proj, ".mcp.json")), "ghp_REALSECRET") {
		t.Fatal("sync should expand ${GH_GITHUB_TOKEN} from secrets.env")
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	t.Setenv("MCPS_REGISTRY", "")
	os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"mcpServers":{"a":{"type":"http","url":"http://a"}}}`), 0o644)
	if run([]string{"import", "--yes", "--dry-run"}) != 0 {
		t.Fatal("dry run failed")
	}
	if strings.Contains(readFile(registryPath()), "[servers.a]") || exists(secretsPath()) {
		t.Fatal("dry-run must not write")
	}
}

func TestDesktopMcpRemoteIsReadBackAsRemote(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	t.Setenv("MCPS_REGISTRY", "")
	app := filepath.Join(h, "Library/Application Support/Claude")
	os.MkdirAll(app, 0o755)
	os.WriteFile(filepath.Join(h, ".claude.json"), []byte(`{"mcpServers":{"b":{"type":"http","url":"http://localhost:3025/mcp"}}}`), 0o644)
	os.WriteFile(filepath.Join(app, "claude_desktop_config.json"), []byte(`{"mcpServers":{"b":{"command":"npx","args":["-y","mcp-remote","http://localhost:3025/mcp"]}}}`), 0o644)
	found, _, err := scanGlobal("")
	if err != nil {
		t.Fatal(err)
	}
	if c := found["b"]; c == nil || len(c.differs) != 0 || c.spec().URL != "http://localhost:3025/mcp" {
		t.Fatalf("desktop mcp-remote entry should equal the remote one: %+v", found["b"])
	}
}

func TestAddAndRemoveRoundTrip(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	t.Setenv("MCPS_REGISTRY", "")
	if run([]string{"add", "api", "--url", "https://x.example/mcp", "-H", "x-api-key: SUPERSECRET"}) != 0 {
		t.Fatal("add url failed")
	}
	if run([]string{"add", "gh", "-e", "GITHUB_TOKEN=ghp_abc", "-e", "LOG=debug", "--", "npx", "-y", "@mcp/github"}) != 0 {
		t.Fatal("add cmd failed")
	}
	reg := readFile(registryPath())
	for _, leaked := range []string{"SUPERSECRET", "ghp_abc"} {
		if strings.Contains(reg, leaked) {
			t.Fatalf("%q leaked into the list:\n%s", leaked, reg)
		}
	}
	for _, want := range []string{"[servers.api]", "[servers.gh]", "${API_X_API_KEY}", "${GH_GITHUB_TOKEN}", `LOG = "debug"`} {
		if !strings.Contains(reg, want) {
			t.Fatalf("missing %q:\n%s", want, reg)
		}
	}
	if run([]string{"add", "gh", "--url", "http://dup"}) == 0 {
		t.Fatal("duplicate add must fail")
	}
	if run([]string{"validate"}) != 0 {
		t.Fatal("list must validate")
	}
	if run([]string{"remove", "gh"}) != 0 {
		t.Fatal("remove failed")
	}
	reg = readFile(registryPath())
	if strings.Contains(reg, "[servers.gh]") || !strings.Contains(reg, "[servers.api]") {
		t.Fatalf("remove must drop only gh:\n%s", reg)
	}
	if run([]string{"remove", "nope"}) == 0 {
		t.Fatal("removing an unknown server must fail")
	}
	// a project can use what was added, secrets resolved from secrets.env
	proj := t.TempDir()
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use=["api"]`), 0o644)
	if run([]string{"sync", "--dir", proj, "--agents", "claude"}) != 0 {
		t.Fatal("sync failed")
	}
	if !strings.Contains(readFile(filepath.Join(proj, ".mcp.json")), "SUPERSECRET") {
		t.Fatal("header value should be filled from secrets.env")
	}
}
