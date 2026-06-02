package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/fsys"
)

func newBeadsPreflightChecker(cityPath, provider string) contract.PreflightChecker {
	return contract.PreflightChecker{
		FS:                fsys.OSFS{},
		Provider:          provider,
		BDContext:         preflightBDContextReader(cityPath),
		DatabaseProjectID: preflightDatabaseProjectIDReader(cityPath),
	}
}

func preflightBDContextReader(cityPath string) func(scope string) (contract.PreflightBDContext, error) {
	return func(scope string) (contract.PreflightBDContext, error) {
		runner := bdCommandRunnerForCity(cityPath)
		out, err := runner(scope, "bd", "context", "--json")
		if err != nil {
			if !bdContextFailureIsNonGitScope(out, err) {
				return contract.PreflightBDContext{}, err
			}
			ctx, fallbackErr := synthesizeBDContextForNonGitScope(scope, runner)
			if fallbackErr != nil {
				return contract.PreflightBDContext{}, fmt.Errorf("%w; synthesize non-git bd context: %w", err, fallbackErr)
			}
			return ctx, nil
		}
		return parsePreflightBDContext(out)
	}
}

func parsePreflightBDContext(out []byte) (contract.PreflightBDContext, error) {
	var raw struct {
		Backend       string `json:"backend"`
		DoltMode      string `json:"dolt_mode"`
		BDVersion     string `json:"bd_version"`
		SchemaVersion int    `json:"schema_version"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return contract.PreflightBDContext{}, fmt.Errorf("parse bd context --json: %w", err)
	}
	return contract.PreflightBDContext{
		Backend:       raw.Backend,
		DoltMode:      raw.DoltMode,
		BDVersion:     raw.BDVersion,
		SchemaVersion: raw.SchemaVersion,
	}, nil
}

func bdContextFailureIsNonGitScope(out []byte, err error) bool {
	message := strings.ToLower(strings.Join([]string{string(out), err.Error()}, "\n"))
	return strings.Contains(message, "cannot resolve repo context") &&
		strings.Contains(message, "not a git repository")
}

func synthesizeBDContextForNonGitScope(scope string, runner func(string, string, ...string) ([]byte, error)) (contract.PreflightBDContext, error) {
	metadata, err := readPreflightScopeMetadata(scope)
	if err != nil {
		return contract.PreflightBDContext{}, err
	}
	statusOut, err := runner(scope, "bd", "status", "--json")
	if err != nil {
		return contract.PreflightBDContext{}, fmt.Errorf("bd status --json: %w", err)
	}
	var status struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(statusOut, &status); err != nil {
		return contract.PreflightBDContext{}, fmt.Errorf("parse bd status --json: %w", err)
	}
	versionOut, err := runner(scope, "bd", "version")
	if err != nil {
		return contract.PreflightBDContext{}, fmt.Errorf("bd version: %w", err)
	}
	version := parseBDVersionOutput(versionOut)
	if version == "" {
		return contract.PreflightBDContext{}, fmt.Errorf("parse bd version output %q", strings.TrimSpace(string(versionOut)))
	}
	return contract.PreflightBDContext{
		Backend:       metadata.Backend,
		DoltMode:      metadata.DoltMode,
		BDVersion:     version,
		SchemaVersion: status.SchemaVersion,
	}, nil
}

type preflightScopeMetadata struct {
	Backend  string `json:"backend"`
	DoltMode string `json:"dolt_mode"`
}

func readPreflightScopeMetadata(scope string) (preflightScopeMetadata, error) {
	path := filepath.Join(scope, ".beads", "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return preflightScopeMetadata{}, fmt.Errorf("read %s: %w", path, err)
	}
	var metadata preflightScopeMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return preflightScopeMetadata{}, fmt.Errorf("parse %s: %w", path, err)
	}
	metadata.Backend = strings.TrimSpace(metadata.Backend)
	metadata.DoltMode = strings.TrimSpace(metadata.DoltMode)
	return metadata, nil
}

func parseBDVersionOutput(out []byte) string {
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "version" && i+1 < len(fields) {
			return strings.TrimPrefix(strings.TrimSpace(fields[i+1]), "v")
		}
	}
	for _, field := range fields {
		trimmed := strings.Trim(strings.TrimPrefix(field, "v"), "()[]{}.,;")
		if strings.Count(trimmed, ".") >= 2 {
			return trimmed
		}
	}
	return ""
}

func preflightDatabaseProjectIDReader(cityPath string) func(scope string) (string, bool, error) {
	return func(scope string) (string, bool, error) {
		target, ok, err := canonicalScopeDoltTarget(cityPath, scope)
		if err != nil || !ok {
			return "", false, err
		}
		db, err := managedDoltOpenDatabase(target.Host, target.Port, target.User, target.Database)
		if err != nil {
			return "", false, err
		}
		defer db.Close() //nolint:errcheck // read-only best-effort close

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return "", false, err
		}
		return readDatabaseProjectID(ctx, db)
	}
}
