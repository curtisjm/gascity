package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/clock"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/runtime"
)

func TestReconcileSessionBeads_NudgesActiveNamedSessionWhenAssignedWorkAppears(t *testing.T) {
	cityPath := t.TempDir()
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	clk := &clock.Fake{Time: time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)}
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Agents: []config.Agent{{
			Name:         "worker",
			StartCommand: "true",
		}},
		NamedSessions: []config.NamedSession{{
			Name:     "primary",
			Template: "worker",
			Mode:     "on_demand",
		}},
	}
	identity := "primary"
	sessionName := config.NamedSessionRuntimeName(cfg.EffectiveCityName(), cfg.Workspace, identity)

	if err := sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("Start(%s): %v", sessionName, err)
	}
	sp.SetActivity(sessionName, clk.Now().Add(-10*time.Second))

	sessionBead, err := store.Create(beads.Bead{
		Title:  sessionName,
		Type:   sessionBeadType,
		Labels: []string{sessionBeadLabel},
		Metadata: map[string]string{
			"session_name":               sessionName,
			"alias":                      identity,
			"template":                   "worker",
			"state":                      "active",
			"last_woke_at":               clk.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"generation":                 "1",
			"instance_token":             "canonical-token",
			namedSessionMetadataKey:      "true",
			namedSessionIdentityMetadata: identity,
			namedSessionModeMetadata:     "on_demand",
		},
	})
	if err != nil {
		t.Fatalf("Create(session): %v", err)
	}
	work, err := store.Create(beads.Bead{
		Title:    "queued assigned work",
		Type:     "task",
		Status:   "open",
		Assignee: identity,
		Metadata: map[string]string{"branch": "polecat/queued-work"},
	})
	if err != nil {
		t.Fatalf("Create(work): %v", err)
	}
	sessions, err := loadSessionBeads(store)
	if err != nil {
		t.Fatalf("loadSessionBeads: %v", err)
	}
	desiredState := map[string]TemplateParams{
		sessionName: {
			Command:                 "true",
			SessionName:             sessionName,
			TemplateName:            "worker",
			ConfiguredNamedIdentity: identity,
			ConfiguredNamedMode:     "on_demand",
		},
	}
	cfgNames := configuredSessionNames(cfg, cfg.EffectiveCityName(), store)
	poolDesired := map[string]int{}
	namedDemand := map[string]bool{identity: true}
	var stdout, stderr bytes.Buffer

	woken := reconcileSessionBeadsAtPathWithNamedDemand(
		context.Background(), cityPath, sessions, desiredState, cfgNames, cfg, sp,
		store, nil, []beads.Bead{work}, nil, nil, newDrainTracker(), poolDesired,
		namedDemand, false, nil, cfg.EffectiveCityName(), nil, clk, events.Discard,
		0, 0, &stdout, &stderr,
	)
	if woken != 0 {
		t.Fatalf("woken = %d, want 0 for already-live named session", woken)
	}
	firstNudges := nudgeNowCalls(sp, sessionName)
	if len(firstNudges) != 1 {
		t.Fatalf("NudgeNow calls after first reconcile = %d, want 1; calls=%+v stderr=%s", len(firstNudges), sp.Calls, stderr.String())
	}
	if !strings.Contains(firstNudges[0].Message, "gc hook") || !strings.Contains(firstNudges[0].Message, work.ID) {
		t.Fatalf("nudge message = %q, want gc hook and work id %s", firstNudges[0].Message, work.ID)
	}

	sessions, err = loadSessionBeads(store)
	if err != nil {
		t.Fatalf("reload session beads: %v", err)
	}
	reconcileSessionBeadsAtPathWithNamedDemand(
		context.Background(), cityPath, sessions, desiredState, cfgNames, cfg, sp,
		store, nil, []beads.Bead{work}, nil, nil, newDrainTracker(), poolDesired,
		namedDemand, false, nil, cfg.EffectiveCityName(), nil, clk, events.Discard,
		0, 0, &stdout, &stderr,
	)
	if got := len(nudgeNowCalls(sp, sessionName)); got != 1 {
		t.Fatalf("NudgeNow calls after repeated reconcile = %d, want still 1; calls=%+v", got, sp.Calls)
	}

	stored, err := store.Get(sessionBead.ID)
	if err != nil {
		t.Fatalf("Get(session): %v", err)
	}
	if stored.Metadata[sessionAssignedWorkNudgeSignatureMetadata] == "" {
		t.Fatal("assigned work nudge signature metadata was not recorded")
	}
}

func TestReconcileSessionBeads_ClearsAssignedWorkNudgeMarkerWhenNoMatchingWork(t *testing.T) {
	cityPath := t.TempDir()
	store := beads.NewMemStore()
	sp := runtime.NewFake()
	clk := &clock.Fake{Time: time.Date(2026, 5, 30, 8, 30, 0, 0, time.UTC)}
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Agents: []config.Agent{{
			Name:         "worker",
			StartCommand: "true",
		}},
		NamedSessions: []config.NamedSession{{
			Name:     "primary",
			Template: "worker",
			Mode:     "on_demand",
		}},
	}
	identity := "primary"
	sessionName := config.NamedSessionRuntimeName(cfg.EffectiveCityName(), cfg.Workspace, identity)
	if err := sp.Start(context.Background(), sessionName, runtime.Config{Command: "true"}); err != nil {
		t.Fatalf("Start(%s): %v", sessionName, err)
	}
	sp.SetActivity(sessionName, clk.Now().Add(-10*time.Second))

	sessionBead, err := store.Create(beads.Bead{
		Title:  sessionName,
		Type:   sessionBeadType,
		Labels: []string{sessionBeadLabel},
		Metadata: map[string]string{
			"session_name":               sessionName,
			"alias":                      identity,
			"template":                   "worker",
			"state":                      "active",
			"last_woke_at":               clk.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			"generation":                 "1",
			"instance_token":             "canonical-token",
			namedSessionMetadataKey:      "true",
			namedSessionIdentityMetadata: identity,
			namedSessionModeMetadata:     "on_demand",
			sessionAssignedWorkNudgeSignatureMetadata: "stale-signature",
			sessionAssignedWorkNudgedAtMetadata:       clk.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		t.Fatalf("Create(session): %v", err)
	}
	sessions, err := loadSessionBeads(store)
	if err != nil {
		t.Fatalf("loadSessionBeads: %v", err)
	}
	desiredState := map[string]TemplateParams{
		sessionName: {
			Command:                 "true",
			SessionName:             sessionName,
			TemplateName:            "worker",
			ConfiguredNamedIdentity: identity,
			ConfiguredNamedMode:     "on_demand",
		},
	}
	cfgNames := configuredSessionNames(cfg, cfg.EffectiveCityName(), store)
	var stdout, stderr bytes.Buffer

	reconcileSessionBeadsAtPathWithNamedDemand(
		context.Background(), cityPath, sessions, desiredState, cfgNames, cfg, sp,
		store, nil, nil, nil, nil, newDrainTracker(), map[string]int{},
		map[string]bool{}, false, nil, cfg.EffectiveCityName(), nil, clk, events.Discard,
		0, 0, &stdout, &stderr,
	)

	stored, err := store.Get(sessionBead.ID)
	if err != nil {
		t.Fatalf("Get(session): %v", err)
	}
	if got := stored.Metadata[sessionAssignedWorkNudgeSignatureMetadata]; got != "" {
		t.Fatalf("assigned_work_nudge_signature = %q, want cleared", got)
	}
	if got := stored.Metadata[sessionAssignedWorkNudgedAtMetadata]; got != "" {
		t.Fatalf("assigned_work_nudged_at = %q, want cleared", got)
	}
	if got := len(nudgeNowCalls(sp, sessionName)); got != 0 {
		t.Fatalf("NudgeNow calls while clearing stale marker = %d, want 0; calls=%+v", got, sp.Calls)
	}
}

func TestAssignedWorkNudgeSignatureUsesConfiguredNamedSessionFallback(t *testing.T) {
	cfg := &config.City{
		Workspace: config.Workspace{Name: "test-city"},
		Agents: []config.Agent{{
			Name:         "worker",
			StartCommand: "true",
		}},
		NamedSessions: []config.NamedSession{{
			Name:     "primary",
			Template: "worker",
			Mode:     "on_demand",
		}},
	}
	identity := "primary"
	sessionName := config.NamedSessionRuntimeName(cfg.EffectiveCityName(), cfg.Workspace, identity)
	session := beads.Bead{
		ID:   "session-bead",
		Type: sessionBeadType,
		Metadata: map[string]string{
			"session_name":          sessionName,
			"template":              "worker",
			namedSessionMetadataKey: "true",
			// Older recovered beads can be missing namedSessionIdentityMetadata;
			// the nudge path must mirror normal awake/assignment fallback matching.
		},
	}
	work := beads.Bead{ID: "work-1", Type: "task", Status: "open", Assignee: identity}

	sig, ids := assignedWorkNudgeSignature(session, cfg, []beads.Bead{work})
	if sig == "" {
		t.Fatal("assignedWorkNudgeSignature returned empty signature")
	}
	if len(ids) != 1 || ids[0] != work.ID {
		t.Fatalf("ids = %#v, want [%s]", ids, work.ID)
	}
}

func nudgeNowCalls(sp *runtime.Fake, sessionName string) []runtime.Call {
	var calls []runtime.Call
	for _, call := range sp.Calls {
		if call.Method == "NudgeNow" && call.Name == sessionName {
			calls = append(calls, call)
		}
	}
	return calls
}
