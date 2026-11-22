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

// A tool that is not installed but already has a project file must be rewritten too,
// otherwise its old servers survive "mcps use".
func TestUseCleansFilesOfUninstalledTools(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	os.MkdirAll(filepath.Join(h, ".claude"), 0o755) // only Claude is installed
	reg := filepath.Join(h, "s.toml")
	os.WriteFile(reg, []byte("[servers.a]\nurl = \"http://a\"\n"), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	proj := filepath.Join(h, "p")
	os.MkdirAll(filepath.Join(proj, ".cursor"), 0o755)
	cur := filepath.Join(proj, ".cursor/mcp.json")
	os.WriteFile(cur, []byte(`{"mcpServers":{"xcode":{"command":"xcrun","args":["mcpbridge"]}}}`), 0o644)

	testPick = true
	defer func() { testPick = false }()
	var toolNotes map[string]string
	pickFn = func(secs []pickSection, _ string) (map[string]bool, error) {
		res := map[string]bool{}
		if strings.HasPrefix(secs[0].Title, "Which AI tools") { // step 1: accept the defaults
			toolNotes = map[string]string{}
			for _, it := range secs[0].Items {
				res[it.ID] = it.On
				toolNotes[it.ID] = it.Note
			}
			return res, nil
		}
		for _, s := range secs {
			for _, it := range s.Items {
				res[it.ID] = it.ID == "a" // untick everything except a
			}
		}
		return res, nil
	}
	defer func() { pickFn = runPicker }()
	if run([]string{"use", "--dir", proj}) != 0 {
		t.Fatal("use failed")
	}
	if !strings.Contains(toolNotes["cursor"], "has a file") || !strings.Contains(readFile(filepath.Join(proj, ".mcps.toml")), "cursor") {
		t.Fatalf("step 1 should pre-tick cursor and save it: %v", toolNotes)
	}
	if got := readFile(cur); strings.Contains(got, "xcode") || !strings.Contains(got, `"a"`) {
		t.Fatalf("old cursor entry survived:\n%s", got)
	}
}
