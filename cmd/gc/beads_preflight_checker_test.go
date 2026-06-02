package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightBDContextReaderSynthesizesNonGitCityContext(t *testing.T) {
	t.Setenv("GC_BEADS", "bd")
	t.Setenv("GC_DOLT", "skip")

	cityDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cityDir, ".beads"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(`[workspace]
name = "demo"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, ".beads", "metadata.json"), []byte(`{
  "backend": "dolt",
  "dolt_database": "hq",
  "dolt_mode": "server",
  "project_id": "project-id"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, ".beads", "config.yaml"), []byte(`issue_prefix: de
gc.endpoint_origin: managed_city
gc.endpoint_status: verified
dolt.auto-start: false
`), 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	bdPath := filepath.Join(binDir, "bd")
	if err := os.WriteFile(bdPath, []byte(`#!/bin/sh
set -eu
case "$*" in
  "context --json")
    printf '{"error":"cannot resolve repo context: cannot determine repository root: not a git repository: exit status 128","schema_version":1}\n'
    exit 1
    ;;
  "status --json")
    printf '{"schema_version":1,"summary":{"total_issues":0}}\n'
    ;;
  "version")
    printf 'bd version 1.0.5 (Homebrew)\n'
    ;;
  *)
    printf 'unexpected bd args: %s\n' "$*" >&2
    exit 99
    ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, err := preflightBDContextReader(cityDir)(cityDir)
	if err != nil {
		t.Fatalf("preflightBDContextReader() error = %v", err)
	}
	if ctx.Backend != "dolt" {
		t.Fatalf("Backend = %q, want dolt", ctx.Backend)
	}
	if ctx.DoltMode != "server" {
		t.Fatalf("DoltMode = %q, want server", ctx.DoltMode)
	}
	if ctx.BDVersion != "1.0.5" {
		t.Fatalf("BDVersion = %q, want 1.0.5", ctx.BDVersion)
	}
	if ctx.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", ctx.SchemaVersion)
	}
}
