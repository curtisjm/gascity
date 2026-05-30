package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/orders"
)

type orderRunStateCountingStore struct {
	beads.Store

	stateScans  int
	labelScans  int
	parentScans int
}

func (s *orderRunStateCountingStore) List(query beads.ListQuery) ([]beads.Bead, error) {
	if query.AllowScan && query.IncludeClosed && query.TierMode == beads.TierBoth && query.Label == "" && query.ParentID == "" {
		s.stateScans++
	}
	if strings.HasPrefix(query.Label, "order-run:") || strings.HasPrefix(query.Label, "order:") {
		s.labelScans++
	}
	if query.ParentID != "" {
		s.parentScans++
	}
	return s.Store.List(query)
}

func TestOrderCheckBatchesRunStateAcrossLegacyStores(t *testing.T) {
	rigStore := &orderRunStateCountingStore{Store: beads.NewMemStore()}
	legacyStore := &orderRunStateCountingStore{Store: beads.NewMemStore()}

	if _, err := legacyStore.Create(beads.Bead{
		Title:  "legacy rig cooldown run",
		Labels: []string{"order-run:digest:rig:frontend"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyStore.Create(beads.Bead{
		Title:  "legacy event cursor",
		Labels: []string{"order:watch:rig:frontend", "seq:2"},
	}); err != nil {
		t.Fatal(err)
	}

	eventLog := events.NewFake()
	eventLog.Record(events.Event{Type: events.BeadClosed, Actor: "test"})
	eventLog.Record(events.Event{Type: events.BeadClosed, Actor: "test"})

	aa := []orders.Order{
		{Name: "digest", Rig: "frontend", Trigger: "cooldown", Interval: "24h", Formula: "mol-digest"},
		{Name: "watch", Rig: "frontend", Trigger: "event", On: events.BeadClosed, Formula: "mol-watch"},
	}
	resolver := func(a orders.Order) ([]beads.Store, error) {
		if a.Rig == "frontend" {
			return []beads.Store{rigStore, legacyStore}, nil
		}
		return []beads.Store{rigStore}, nil
	}

	var stdout, stderr bytes.Buffer
	code := doOrderCheckWithStoresResolver(aa, time.Now().Add(time.Second), eventLog, resolver, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("doOrderCheckWithStoresResolver = %d, want 1 (both orders suppressed); stderr: %s; stdout: %s", code, stderr.String(), stdout.String())
	}
	if got := stdout.String(); !strings.Contains(got, "digest") || !strings.Contains(got, "watch") || !strings.Contains(got, "no") {
		t.Fatalf("stdout missing expected not-due rows:\n%s", got)
	}
	if rigStore.stateScans != 1 || legacyStore.stateScans != 1 {
		t.Fatalf("state scans rig=%d legacy=%d, want one batched scan per store", rigStore.stateScans, legacyStore.stateScans)
	}
	if rigStore.labelScans != 0 || legacyStore.labelScans != 0 {
		t.Fatalf("per-order label scans rig=%d legacy=%d, want batched run-state lookup", rigStore.labelScans, legacyStore.labelScans)
	}
}

func TestOrderDispatchBatchesOpenWorkHistoryAndCursors(t *testing.T) {
	store := &orderRunStateCountingStore{Store: beads.NewMemStore()}
	if _, err := store.Create(beads.Bead{
		Title:  "blocked tracking",
		Labels: []string{"order-run:blocked", labelOrderTracking},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(beads.Bead{
		Title:  "recent cooldown run",
		Labels: []string{"order-run:recent"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(beads.Bead{
		Title:  "event cursor",
		Labels: []string{"order:watch", "seq:1"},
	}); err != nil {
		t.Fatal(err)
	}

	eventLog := events.NewFake()
	eventLog.Record(events.Event{Type: events.BeadClosed, Actor: "test"})

	var calls int
	execRun := func(context.Context, string, string, []string) ([]byte, error) {
		calls++
		return nil, nil
	}
	ad := buildOrderDispatcherFromListExec([]orders.Order{
		{Name: "blocked", Trigger: "cooldown", Interval: "1m", Exec: "true"},
		{Name: "recent", Trigger: "cooldown", Interval: "1h", Exec: "true"},
		{Name: "watch", Trigger: "event", On: events.BeadClosed, Exec: "true"},
	}, store, eventLog, execRun, nil)
	if ad == nil {
		t.Fatal("expected non-nil dispatcher")
	}

	ad.dispatch(context.Background(), t.TempDir(), time.Now().Add(time.Second))
	ad.drain(context.Background())

	if calls != 0 {
		t.Fatalf("exec calls = %d, want 0 (open work, cooldown history, and cursor should suppress dispatch)", calls)
	}
	if store.stateScans != 1 {
		t.Fatalf("state scans = %d, want one batched scan for all orders", store.stateScans)
	}
	if store.labelScans != 0 {
		t.Fatalf("per-order label scans = %d, want batched run-state lookup", store.labelScans)
	}
}
