package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/orders"
)

type orderRunStateCache struct {
	labels  []string
	entries []orderRunStateCacheEntry
}

// orderRunStateLabelLister is an optional beads.Store fast path implemented by
// stores that can batch exact-label reads in one backend query.
type orderRunStateLabelLister interface {
	ListAnyLabel(labels []string, query beads.ListQuery) ([]beads.Bead, error)
}

type orderRunStateCacheEntry struct {
	store beads.Store
	state *orderRunStoreState
	err   error
}

type orderRunStoreState struct {
	lastRun       map[string]time.Time
	cursor        map[string]uint64
	openWork      map[string]bool
	openWispRoots map[string]map[string]struct{}
}

func newOrderRunStateCache(labels ...string) *orderRunStateCache {
	return &orderRunStateCache{labels: dedupeOrderRunStateLabels(labels)}
}

func newOrderRunStateCacheForOrders(aa []orders.Order) *orderRunStateCache {
	labels := make([]string, 0, len(aa)*2)
	for _, order := range aa {
		scoped := order.ScopedName()
		if scoped == "" {
			continue
		}
		labels = append(labels, "order-run:"+scoped, "order:"+scoped)
	}
	return newOrderRunStateCache(labels...)
}

func dedupeOrderRunStateLabels(labels []string) []string {
	seen := make(map[string]struct{}, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func newOrderRunStoreState() *orderRunStoreState {
	return &orderRunStoreState{
		lastRun:  make(map[string]time.Time),
		cursor:   make(map[string]uint64),
		openWork: make(map[string]bool),
	}
}

func (c *orderRunStateCache) stateForStore(store beads.Store) (*orderRunStoreState, error) {
	if store == nil {
		return newOrderRunStoreState(), nil
	}
	for i := range c.entries {
		if sameOrderRunStateStore(c.entries[i].store, store) {
			return c.entries[i].state, c.entries[i].err
		}
	}
	state, err := loadOrderRunStoreState(store, c.labels)
	c.entries = append(c.entries, orderRunStateCacheEntry{store: store, state: state, err: err})
	return state, err
}

func sameOrderRunStateStore(a, b beads.Store) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

func loadOrderRunStoreState(store beads.Store, labels []string) (*orderRunStoreState, error) {
	query := beads.ListQuery{
		IncludeClosed: true,
		AllowScan:     true,
		Sort:          beads.SortCreatedDesc,
		TierMode:      beads.TierBoth,
	}
	var rows []beads.Bead
	var err error
	if lister, ok := store.(orderRunStateLabelLister); ok && len(labels) > 0 {
		rows, err = lister.ListAnyLabel(labels, query)
	} else {
		rows, err = store.List(query)
		if len(labels) > 0 {
			rows = filterOrderRunStateRows(rows, labels)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("listing order run state: %w", err)
	}
	state := newOrderRunStoreState()
	for _, row := range rows {
		state.observe(row)
	}
	if err := state.resolveOpenWispRoots(store); err != nil {
		return nil, err
	}
	return state, nil
}

func filterOrderRunStateRows(rows []beads.Bead, labels []string) []beads.Bead {
	if len(labels) == 0 || len(rows) == 0 {
		return rows
	}
	want := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		want[label] = struct{}{}
	}
	out := rows[:0]
	for _, row := range rows {
		if beadLabelsIntersect(row.Labels, want) {
			out = append(out, row)
		}
	}
	return out
}

func beadLabelsIntersect(labels []string, want map[string]struct{}) bool {
	for _, label := range labels {
		if _, ok := want[label]; ok {
			return true
		}
	}
	return false
}

func (s *orderRunStoreState) observe(b beads.Bead) {
	seq := orders.MaxSeqFromLabels([][]string{b.Labels})
	seenOrderRun := make(map[string]struct{})
	seenCursor := make(map[string]struct{})
	for _, label := range b.Labels {
		if scoped, ok := strings.CutPrefix(label, "order-run:"); ok && scoped != "" {
			if _, seen := seenOrderRun[scoped]; !seen {
				s.rememberLastRun(scoped, b.CreatedAt)
				s.rememberOpenWork(scoped, b)
				seenOrderRun[scoped] = struct{}{}
			}
			continue
		}
		if scoped, ok := strings.CutPrefix(label, "order:"); ok && scoped != "" && seq > 0 {
			if _, seen := seenCursor[scoped]; !seen {
				if seq > s.cursor[scoped] {
					s.cursor[scoped] = seq
				}
				seenCursor[scoped] = struct{}{}
			}
		}
	}
}

func (s *orderRunStoreState) rememberLastRun(scoped string, at time.Time) {
	if at.IsZero() {
		return
	}
	if existing, ok := s.lastRun[scoped]; !ok || at.After(existing) {
		s.lastRun[scoped] = at
	}
}

func (s *orderRunStoreState) rememberOpenWork(scoped string, b beads.Bead) {
	if b.Status == "closed" {
		return
	}
	if beadLabelsContain(b.Labels, labelOrderTracking) {
		s.openWork[scoped] = true
		return
	}
	if !isOrderWispRootCandidate(b) {
		return
	}
	if isOrderRootOnlyWispCandidate(b) {
		s.openWork[scoped] = true
		return
	}
	if s.openWispRoots == nil {
		s.openWispRoots = make(map[string]map[string]struct{})
	}
	ids := s.openWispRoots[scoped]
	if ids == nil {
		ids = make(map[string]struct{})
		s.openWispRoots[scoped] = ids
	}
	ids[b.ID] = struct{}{}
}

func (s *orderRunStoreState) resolveOpenWispRoots(store beads.Store) error {
	for scoped, ids := range s.openWispRoots {
		if s.openWork[scoped] {
			continue
		}
		for id := range ids {
			hasOpenDescendants, err := storeHasOpenDescendants(store, id)
			if err != nil {
				return fmt.Errorf("checking open descendants of wisp %s: %w", id, err)
			}
			if hasOpenDescendants {
				s.openWork[scoped] = true
				break
			}
		}
	}
	return nil
}

func (c *orderRunStateCache) lastRunFunc(stores []beads.Store) orders.LastRunFunc {
	return func(orderName string) (time.Time, error) {
		return c.lastRunAcrossStores(orderName, stores...)
	}
}

func (c *orderRunStateCache) lastRunAcrossStores(orderName string, stores ...beads.Store) (time.Time, error) {
	var latest time.Time
	for i, store := range stores {
		state, err := c.stateForStore(store)
		if err != nil {
			return time.Time{}, fmt.Errorf("store %d: %w", i, err)
		}
		if state == nil {
			continue
		}
		if last := state.lastRun[orderName]; last.After(latest) {
			latest = last
		}
	}
	return latest, nil
}

func (c *orderRunStateCache) cursorAcrossStores(orderName string, stores ...beads.Store) (uint64, error) {
	var maxSeq uint64
	for i, store := range stores {
		state, err := c.stateForStore(store)
		if err != nil {
			return 0, fmt.Errorf("store %d: %w", i, err)
		}
		if state == nil {
			continue
		}
		if seq := state.cursor[orderName]; seq > maxSeq {
			maxSeq = seq
		}
	}
	return maxSeq, nil
}

func (c *orderRunStateCache) hasOpenWorkInStores(stores []beads.Store, scopedName string) (bool, error) {
	for i, store := range stores {
		state, err := c.stateForStore(store)
		if err != nil {
			return false, fmt.Errorf("store %d: %w", i, err)
		}
		if state != nil && state.openWork[scopedName] {
			return true, nil
		}
	}
	return false, nil
}

func (c *orderRunStateCache) rememberOpenRun(store beads.Store, scopedName string, createdAt time.Time) {
	state, err := c.stateForStore(store)
	if err != nil || state == nil {
		return
	}
	state.rememberLastRun(scopedName, createdAt)
	state.openWork[scopedName] = true
}
