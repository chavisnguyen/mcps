package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUseReplacesFolderAndPromotes(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	reg := filepath.Join(h, "s.toml")
	os.WriteFile(reg, []byte("[servers.a]\nurl = \"http://hub-a\"\n[servers.c]\nurl = \"http://hub-c\"\n"), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	proj := filepath.Join(h, "old")
	os.MkdirAll(proj, 0o755)
	mcp := filepath.Join(proj, ".mcp.json")
	os.WriteFile(mcp, []byte(`{"mcpServers":{
	  "a":{"type":"http","url":"http://old-a"},
	  "b":{"command":"npx","args":["-y","b-pkg"],"env":{"B_TOKEN":"s3cret"}},
	  "z":{"type":"http","url":"http://zzz"}}}`), 0o644)

	testPick = true
	defer func() { testPick = false }()
	var seen []pickSection
	pickFn = func(secs []pickSection, _ string) (map[string]bool, error) {
		seen = secs
		res := map[string]bool{}
		for _, s := range secs {
			for _, it := range s.Items {
				res[it.ID] = it.On && it.ID != "only:z"
			}
		}
		return res, nil
	}
	defer func() { pickFn = runPicker }()

	if run([]string{"use", "--dir", proj, "--agents", "claude"}) != 0 {
		t.Fatal("use failed")
	}
	if len(seen) != 2 || len(seen[0].Items) != 2 || len(seen[1].Items) != 2 {
		t.Fatalf("sections wrong: %+v", seen)
	}
	if !seen[0].Items[0].On || seen[0].Items[1].On {
		t.Fatalf("a (on here) should be ticked, c not: %+v", seen[0].Items)
	}
	if !strings.Contains(seen[0].Items[0].Note, "differs") {
		t.Fatalf("a should be flagged as differing: %q", seen[0].Items[0].Note)
	}
	got := readFile(mcp)
	if strings.Contains(got, `"z"`) || strings.Contains(got, "old-a") || !strings.Contains(got, "hub-a") || !strings.Contains(got, `"b"`) {
		t.Fatalf("folder not rewritten as chosen:\n%s", got)
	}
	r := readFile(reg)
	if !strings.Contains(r, "[servers.b]") || strings.Contains(r, "s3cret") || !strings.Contains(r, "${B_B_TOKEN}") && !strings.Contains(r, "${B_TOKEN}") {
		t.Fatalf("b not promoted safely:\n%s", r)
	}
	if !strings.Contains(readFile(filepath.Join(h, ".mcps/secrets.env")), "s3cret") {
		t.Fatal("secret not stored")
	}
	if p := readFile(filepath.Join(proj, ".mcps.toml")); !strings.Contains(p, `"a"`) || !strings.Contains(p, `"b"`) || strings.Contains(p, `"c"`) {
		t.Fatalf(".mcps.toml wrong:\n%s", p)
	}
}
