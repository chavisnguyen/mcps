package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusDetectsDrift(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("MCPS_HOME", filepath.Join(h, ".mcps"))
	os.MkdirAll(filepath.Join(h, ".cursor"), 0o755)
	reg := filepath.Join(h, "servers.toml")
	os.WriteFile(reg, []byte("[servers.a]\nurl = \"http://localhost:1/mcp\"\n"), 0o644)
	t.Setenv("MCPS_REGISTRY", reg)
	proj := filepath.Join(h, "app")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, ".mcps.toml"), []byte(`use = ["a"]`), 0o644)

	if run([]string{"sync", "--dir", proj, "--agents", "cursor"}) != 0 {
		t.Fatal("sync failed")
	}
	if got := knownProjects(); len(got) != 1 || got[0] != proj {
		t.Fatalf("knownProjects = %v", got)
	}
	if run([]string{"status", "--dir", proj}) != 0 {
		t.Fatal("expected in sync")
	}
	// someone edits the generated file by hand
	f := filepath.Join(proj, ".cursor/mcp.json")
	os.WriteFile(f, []byte(strings.ReplaceAll(readFile(f), "localhost:1", "localhost:2")), 0o644)
	if run([]string{"status", "--dir", proj}) != 1 {
		t.Fatal("expected drift")
	}
	if run([]string{"doctor", "--dir", proj}) != 1 {
		t.Fatal("doctor should report the difference")
	}
	run([]string{"sync", "--dir", proj, "--agents", "cursor"})
	if run([]string{"status", "--dir", proj}) != 0 {
		t.Fatal("sync should fix drift")
	}
}
